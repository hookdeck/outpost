package coordinator

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/migrator"
	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/redislock"
	"go.uber.org/zap"
)

const (
	// redisLockKey mirrors the key used by migratorredis.Runner so the
	// coordinator is interoperable with existing lock state.
	redisLockKey = ".outpost:migration_lock"
	// redisLockTTL matches the existing behaviour: the lock expires
	// after an hour if the process holding it dies.
	redisLockTTL = time.Hour
)

// Coordinator unifies the SQL and Redis migration subsystems behind a
// single API. It is intentionally safe to use from both a CLI and from
// app startup checks.
type Coordinator struct {
	sqlMigrator  *migrator.Migrator
	redisClient  redis.Client
	migrations   []migratorredis.Migration
	deploymentID string
	logger       *logging.Logger
}

// Config bundles the inputs needed to construct a Coordinator. Callers
// are expected to own the lifecycle of the SQL migrator and the Redis
// client and close them when finished.
type Config struct {
	SQLMigrator      *migrator.Migrator
	RedisClient      redis.Client
	RedisMigrations  []migratorredis.Migration
	DeploymentID     string
	Logger           *logging.Logger
}

// New constructs a Coordinator. Either SQLMigrator or the Redis fields
// may be nil if a subsystem is not configured, but at least one must be
// provided for any operation to do meaningful work.
func New(cfg Config) *Coordinator {
	return &Coordinator{
		sqlMigrator:  cfg.SQLMigrator,
		redisClient:  cfg.RedisClient,
		migrations:   sortedByVersion(cfg.RedisMigrations),
		deploymentID: cfg.DeploymentID,
		logger:       cfg.Logger,
	}
}

func sortedByVersion(ms []migratorredis.Migration) []migratorredis.Migration {
	out := make([]migratorredis.Migration, len(ms))
	copy(out, ms)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Version() < out[j].Version()
	})
	return out
}

// List returns the unified view of every migration (SQL + Redis) with
// its current status.
func (c *Coordinator) List(ctx context.Context) ([]MigrationInfo, error) {
	var out []MigrationInfo

	if c.sqlMigrator != nil {
		sqlMigrations, err := c.sqlMigrator.ListMigrations(ctx)
		if err != nil {
			return nil, fmt.Errorf("list sql migrations: %w", err)
		}
		for _, m := range sqlMigrations {
			status := StatusPending
			if m.Applied {
				status = StatusApplied
			}
			out = append(out, MigrationInfo{
				ID:      fmt.Sprintf("sql/%06d", m.Version),
				Type:    MigrationTypeSQL,
				Version: m.Version,
				Name:    m.Name,
				Status:  status,
			})
		}
	}

	if c.redisClient != nil {
		for _, rm := range c.migrations {
			status, reason := c.redisStatus(ctx, rm.Name())
			// If the migration has no stored status yet, consult
			// IsApplicable so list agrees with PendingSummary about
			// whether this row is actually pending. Without this
			// filter, list would report a migration as [pending]
			// while the startup gate (which does filter) treats it
			// as not counting — the two views then disagree.
			if status == StatusPending {
				if applicable, notAppReason := rm.IsApplicable(ctx); !applicable {
					status = StatusNotApplicable
					reason = notAppReason
				}
			}
			out = append(out, MigrationInfo{
				ID:          fmt.Sprintf("redis/%s", rm.Name()),
				Type:        MigrationTypeRedis,
				Version:     rm.Version(),
				Name:        rm.Name(),
				Description: rm.Description(),
				Status:      status,
				Reason:      reason,
			})
		}
	}

	return out, nil
}

// Plan returns the aggregated plan describing what Apply would do.
// It is safe to call with no side effects.
func (c *Coordinator) Plan(ctx context.Context) (*Plan, error) {
	plan := &Plan{}

	if c.sqlMigrator != nil {
		current, err := c.sqlMigrator.Version(ctx)
		if err != nil {
			return nil, fmt.Errorf("sql version: %w", err)
		}
		all, err := c.sqlMigrator.ListMigrations(ctx)
		if err != nil {
			return nil, fmt.Errorf("list sql migrations: %w", err)
		}
		latest := 0
		for _, m := range all {
			if m.Version > latest {
				latest = m.Version
			}
			if !m.Applied {
				plan.SQL.Pending = append(plan.SQL.Pending, SQLMigrationInfo{
					Version: m.Version,
					Name:    m.Name,
				})
			}
		}
		plan.SQL.CurrentVersion = current
		plan.SQL.LatestVersion = latest
		plan.SQL.PendingCount = len(plan.SQL.Pending)
	}

	if c.redisClient != nil {
		for _, rm := range c.migrations {
			if c.isRedisSatisfied(ctx, rm.Name()) {
				continue
			}
			applicable, reason := rm.IsApplicable(ctx)
			if !applicable {
				c.logger.Debug("redis migration not applicable",
					zap.String("migration", rm.Name()),
					zap.String("reason", reason))
				continue
			}
			mp, err := rm.Plan(ctx)
			if err != nil {
				return nil, fmt.Errorf("plan redis migration %s: %w", rm.Name(), err)
			}
			plan.Redis = append(plan.Redis, RedisMigrationPlan{
				Name:           rm.Name(),
				Description:    rm.Description(),
				EstimatedItems: mp.EstimatedItems,
				Scope:          mp.Scope,
			})
		}
	}

	return plan, nil
}

// PendingSummary returns a lightweight count of pending migrations.
// Used by the app startup gate (Phase 4) to decide whether to refuse
// starting the server.
func (c *Coordinator) PendingSummary(ctx context.Context) (PendingSummary, error) {
	var summary PendingSummary

	if c.sqlMigrator != nil {
		count, err := c.sqlMigrator.PendingCount(ctx)
		if err != nil {
			return summary, fmt.Errorf("sql pending: %w", err)
		}
		summary.SQLPending = count
	}

	if c.redisClient != nil {
		for _, rm := range c.migrations {
			if c.isRedisSatisfied(ctx, rm.Name()) {
				continue
			}
			applicable, _ := rm.IsApplicable(ctx)
			if !applicable {
				continue
			}
			summary.RedisPending++
		}
	}

	return summary, nil
}

// Apply runs SQL migrations first (if any pending) then Redis migrations
// in version order. It acquires the Redis distributed lock for the
// Redis phase to prevent concurrent runs.
func (c *Coordinator) Apply(ctx context.Context, opts ApplyOptions) error {
	if c.sqlMigrator != nil && !opts.RedisOnly {
		if err := c.applySQL(ctx); err != nil {
			return err
		}
	}

	if c.redisClient != nil && !opts.SQLOnly {
		if err := c.applyRedis(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (c *Coordinator) applySQL(ctx context.Context) error {
	version, applied, err := c.sqlMigrator.Up(ctx, -1)
	if err != nil {
		return fmt.Errorf("sql migrate up: %w", err)
	}
	c.logger.Info("sql migrations applied",
		zap.Int("version", version),
		zap.Int("count", applied))
	return nil
}

func (c *Coordinator) applyRedis(ctx context.Context) error {
	// Determine which migrations actually need to run. Migrations that
	// are already satisfied (applied or not_applicable) are skipped.
	// Migrations that are not applicable for the current configuration
	// are marked as such without acquiring the lock.
	var pending []migratorredis.Migration
	for _, rm := range c.migrations {
		if c.isRedisSatisfied(ctx, rm.Name()) {
			continue
		}
		applicable, reason := rm.IsApplicable(ctx)
		if !applicable {
			if err := c.markNotApplicable(ctx, rm.Name(), reason); err != nil {
				return fmt.Errorf("mark %s not applicable: %w", rm.Name(), err)
			}
			c.logger.Info("redis migration not applicable",
				zap.String("migration", rm.Name()),
				zap.String("reason", reason))
			continue
		}
		pending = append(pending, rm)
	}

	if len(pending) == 0 {
		return nil
	}

	lock := c.newRedisLock()
	ok, err := lock.AttemptLock(ctx)
	if err != nil {
		return fmt.Errorf("acquire redis migration lock: %w", err)
	}
	if !ok {
		return fmt.Errorf("acquire redis migration lock: lock already held")
	}
	defer func() {
		if _, err := lock.Unlock(ctx); err != nil {
			c.logger.Warn("failed to release redis migration lock", zap.Error(err))
		}
	}()

	for _, rm := range pending {
		// Double-check after taking the lock in case another instance
		// beat us to it.
		if c.isRedisSatisfied(ctx, rm.Name()) {
			continue
		}

		mp, err := rm.Plan(ctx)
		if err != nil {
			return fmt.Errorf("plan %s: %w", rm.Name(), err)
		}

		c.logger.Info("applying redis migration",
			zap.String("migration", rm.Name()),
			zap.Int("estimated_items", mp.EstimatedItems))

		state, err := rm.Apply(ctx, mp)
		if err != nil {
			return fmt.Errorf("apply %s: %w", rm.Name(), err)
		}

		if err := c.markApplied(ctx, rm.Name()); err != nil {
			return fmt.Errorf("mark %s applied: %w", rm.Name(), err)
		}

		c.logger.Info("redis migration applied",
			zap.String("migration", rm.Name()),
			zap.Int("processed", state.Progress.ProcessedItems),
			zap.Int("failed", state.Progress.FailedItems))
	}

	return nil
}

// Verify runs verification across both subsystems. For SQL, it checks
// that the current schema version matches the latest available version.
// For Redis, it runs each applied migration's Verify method.
func (c *Coordinator) Verify(ctx context.Context) (*VerificationReport, error) {
	report := &VerificationReport{}

	if c.sqlMigrator != nil {
		current, err := c.sqlMigrator.Version(ctx)
		if err != nil {
			return nil, fmt.Errorf("sql version: %w", err)
		}
		latest, err := c.sqlMigrator.LatestVersion()
		if err != nil {
			return nil, fmt.Errorf("sql latest: %w", err)
		}
		report.SQLCurrentVersion = current
		report.SQLLatestVersion = latest
	}

	if c.redisClient != nil {
		for _, rm := range c.migrations {
			if !c.isRedisApplied(ctx, rm.Name()) {
				continue
			}
			result, err := rm.Verify(ctx, &migratorredis.State{
				MigrationName: rm.Name(),
				Phase:         "applied",
			})
			if err != nil {
				return nil, fmt.Errorf("verify %s: %w", rm.Name(), err)
			}
			report.RedisResults = append(report.RedisResults, RedisVerifyResult{
				Name:         rm.Name(),
				Valid:        result.Valid,
				ChecksRun:    result.ChecksRun,
				ChecksPassed: result.ChecksPassed,
				Issues:       result.Issues,
				VerifiedAt:   time.Now(),
			})
		}
	}

	return report, nil
}

// Unlock force-clears the Redis migration lock. SQL migrations use
// PostgreSQL advisory locks which release automatically when the
// connection closes, so there is no SQL-side unlock operation.
func (c *Coordinator) Unlock(ctx context.Context) error {
	if c.redisClient == nil {
		return nil
	}
	if err := c.redisClient.Del(ctx, c.redisLockKey()).Err(); err != nil {
		return fmt.Errorf("clear redis migration lock: %w", err)
	}
	c.logger.Info("redis migration lock cleared")
	return nil
}

// Helpers for Redis status tracking. The hash key format and field
// names mirror migratorredis.Runner so existing state is honored.

func (c *Coordinator) redisMigrationKey(name string) string {
	prefix := ""
	if c.deploymentID != "" {
		prefix = c.deploymentID + ":"
	}
	return fmt.Sprintf("%soutpost:migration:%s", prefix, name)
}

func (c *Coordinator) redisLockKey() string {
	prefix := ""
	if c.deploymentID != "" {
		prefix = c.deploymentID + ":"
	}
	return prefix + redisLockKey
}

func (c *Coordinator) newRedisLock() redislock.Lock {
	return redislock.New(c.redisClient,
		redislock.WithKey(c.redisLockKey()),
		redislock.WithTTL(redisLockTTL),
	)
}

func (c *Coordinator) redisStatus(ctx context.Context, name string) (MigrationStatus, string) {
	res, err := c.redisClient.HGetAll(ctx, c.redisMigrationKey(name)).Result()
	if err != nil || len(res) == 0 {
		return StatusPending, ""
	}
	switch res["status"] {
	case "applied":
		return StatusApplied, ""
	case "not_applicable":
		return StatusNotApplicable, res["reason"]
	default:
		return StatusPending, ""
	}
}

func (c *Coordinator) isRedisSatisfied(ctx context.Context, name string) bool {
	status, _ := c.redisStatus(ctx, name)
	return status == StatusApplied || status == StatusNotApplicable
}

func (c *Coordinator) isRedisApplied(ctx context.Context, name string) bool {
	status, _ := c.redisStatus(ctx, name)
	return status == StatusApplied
}

func (c *Coordinator) markApplied(ctx context.Context, name string) error {
	return c.redisClient.HSet(ctx, c.redisMigrationKey(name),
		"status", "applied",
		"applied_at", time.Now().Format(time.RFC3339),
	).Err()
}

func (c *Coordinator) markNotApplicable(ctx context.Context, name, reason string) error {
	return c.redisClient.HSet(ctx, c.redisMigrationKey(name),
		"status", "not_applicable",
		"checked_at", time.Now().Format(time.RFC3339),
		"reason", reason,
	).Err()
}

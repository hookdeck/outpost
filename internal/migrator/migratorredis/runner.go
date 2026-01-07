package migratorredis

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/redislock"
	"go.uber.org/zap"
)

const (
	migrationLockKey = ".outpost:migration:lock"
	lockTTL          = time.Hour // Lock expires after 1 hour in case process dies
)

// Runner handles automatic Redis migrations at startup
type Runner struct {
	client     redis.Client
	logger     *logging.Logger
	migrations []Migration
	lock       redislock.Lock
}

// NewRunner creates a new migration runner
func NewRunner(client redis.Client, logger *logging.Logger) *Runner {
	return &Runner{
		client: client,
		logger: logger,
		lock: redislock.New(client,
			redislock.WithKey(migrationLockKey),
			redislock.WithTTL(lockTTL),
		),
	}
}

// RegisterMigration adds a migration to the runner
func (r *Runner) RegisterMigration(m Migration) {
	r.migrations = append(r.migrations, m)
}

// NewLoggerAdapter creates a Logger adapter from a logging.Logger
func NewLoggerAdapter(logger *logging.Logger, verbose bool) Logger {
	return &loggerAdapter{logger: logger, verbose: verbose}
}

// Run executes all pending migrations automatically
// It handles locking to ensure only one instance runs migrations at a time
func (r *Runner) Run(ctx context.Context) error {
	// Sort migrations by version
	sort.Slice(r.migrations, func(i, j int) bool {
		return r.migrations[i].Version() < r.migrations[j].Version()
	})

	// Check if this is a fresh installation
	isFresh, err := r.checkIfFreshInstallation(ctx)
	if err != nil {
		return fmt.Errorf("failed to check installation status: %w", err)
	}

	if isFresh {
		// Fresh installation - mark all migrations as applied
		return r.handleFreshInstallation(ctx)
	}

	// Existing installation - run pending migrations
	return r.runPendingMigrations(ctx)
}

// checkIfFreshInstallation checks if Redis has any existing Outpost data
func (r *Runner) checkIfFreshInstallation(ctx context.Context) (bool, error) {
	// Check for any "outpost:*" keys (current format)
	outpostKeys, err := r.client.Keys(ctx, "outpost:*").Result()
	if err != nil {
		return false, fmt.Errorf("failed to check outpost keys: %w", err)
	}
	if len(outpostKeys) > 0 {
		return false, nil
	}

	// Check for any "tenant:*" keys (old format)
	tenantKeys, err := r.client.Keys(ctx, "tenant:*").Result()
	if err != nil {
		return false, fmt.Errorf("failed to check tenant keys: %w", err)
	}
	if len(tenantKeys) > 0 {
		return false, nil
	}

	return true, nil
}

// handleFreshInstallation marks all migrations as applied for new installations
func (r *Runner) handleFreshInstallation(ctx context.Context) error {
	r.logger.Info("fresh redis installation detected, marking migrations as applied",
		zap.Int("migrations", len(r.migrations)))

	// Acquire lock
	if err := r.acquireLock(ctx); err != nil {
		// Another instance is initializing, wait for it
		r.logger.Info("another instance is initializing redis, waiting...")
		return r.waitForInitialization(ctx)
	}
	defer r.releaseLock(ctx)

	// Double-check after acquiring lock
	isFresh, err := r.checkIfFreshInstallation(ctx)
	if err != nil {
		return err
	}
	if !isFresh {
		r.logger.Debug("redis already initialized by another instance")
		return nil
	}

	// Mark all migrations as applied
	for _, m := range r.migrations {
		if err := r.setMigrationApplied(ctx, m.Name()); err != nil {
			return fmt.Errorf("failed to mark migration %s as applied: %w", m.Name(), err)
		}
	}

	r.logger.Info("redis initialization complete",
		zap.Int("migrations_marked", len(r.migrations)))
	return nil
}

// PendingMigration contains info about a pending migration
type PendingMigration struct {
	Name        string
	Description string
	AutoRunnable bool
}

// GetPendingMigrations returns a list of all pending migrations
func (r *Runner) GetPendingMigrations(ctx context.Context) []PendingMigration {
	var pending []PendingMigration
	for _, m := range r.migrations {
		if !r.isMigrationApplied(ctx, m.Name()) {
			pending = append(pending, PendingMigration{
				Name:         m.Name(),
				Description:  m.Description(),
				AutoRunnable: m.AutoRunnable(),
			})
		}
	}
	return pending
}

// runPendingMigrations runs any unapplied migrations that are auto-runnable
func (r *Runner) runPendingMigrations(ctx context.Context) error {
	// Find pending migrations
	var pending []Migration
	var notAutoRunnable []string

	for _, m := range r.migrations {
		if !r.isMigrationApplied(ctx, m.Name()) {
			pending = append(pending, m)
			if !m.AutoRunnable() {
				notAutoRunnable = append(notAutoRunnable, m.Name())
			}
		}
	}

	if len(pending) == 0 {
		r.logger.Debug("all redis migrations applied")
		return nil
	}

	// Log all pending migrations
	pendingNames := make([]string, len(pending))
	for i, m := range pending {
		pendingNames[i] = m.Name()
	}
	r.logger.Info("redis migrations pending",
		zap.Int("count", len(pending)),
		zap.Strings("migrations", pendingNames))

	// Check if all pending migrations are auto-runnable
	if len(notAutoRunnable) > 0 {
		r.logger.Warn("some pending migrations require manual intervention",
			zap.Strings("manual_migrations", notAutoRunnable))
		return fmt.Errorf("pending migrations require manual run via 'outpost-migrate-redis apply': %v", notAutoRunnable)
	}

	// Run each pending migration
	for _, m := range pending {
		if err := r.runMigration(ctx, m); err != nil {
			return fmt.Errorf("migration %s failed: %w", m.Name(), err)
		}
	}

	return nil
}

// runMigration executes a single migration with locking
func (r *Runner) runMigration(ctx context.Context, m Migration) error {
	r.logger.Info("running redis migration",
		zap.String("migration", m.Name()),
		zap.String("description", m.Description()))

	// Acquire lock
	if err := r.acquireLock(ctx); err != nil {
		return fmt.Errorf("failed to acquire migration lock: %w", err)
	}
	defer r.releaseLock(ctx)

	// Check if already applied (another instance may have run it)
	if r.isMigrationApplied(ctx, m.Name()) {
		r.logger.Debug("migration already applied by another instance",
			zap.String("migration", m.Name()))
		return nil
	}

	// Plan
	plan, err := m.Plan(ctx)
	if err != nil {
		return fmt.Errorf("plan failed: %w", err)
	}

	r.logger.Info("migration plan",
		zap.String("migration", m.Name()),
		zap.Int("estimated_items", plan.EstimatedItems))

	// Apply
	state, err := m.Apply(ctx, plan)
	if err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}

	// Mark as applied
	if err := r.setMigrationApplied(ctx, m.Name()); err != nil {
		return fmt.Errorf("failed to mark migration as applied: %w", err)
	}

	r.logger.Info("redis migration completed",
		zap.String("migration", m.Name()),
		zap.Int("processed", state.Progress.ProcessedItems),
		zap.Int("failed", state.Progress.FailedItems))

	return nil
}

// acquireLock attempts to acquire the migration lock using infra.Lock
func (r *Runner) acquireLock(ctx context.Context) error {
	success, err := r.lock.AttemptLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !success {
		return fmt.Errorf("lock already held")
	}

	return nil
}

// releaseLock releases the migration lock using infra.Lock
func (r *Runner) releaseLock(ctx context.Context) {
	if _, err := r.lock.Unlock(ctx); err != nil {
		r.logger.Warn("failed to release migration lock", zap.Error(err))
	}
}

// waitForInitialization waits for another instance to complete initialization
func (r *Runner) waitForInitialization(ctx context.Context) error {
	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}

		isFresh, err := r.checkIfFreshInstallation(ctx)
		if err != nil {
			return err
		}
		if !isFresh {
			r.logger.Debug("redis initialized by another instance")
			return nil
		}
	}

	return fmt.Errorf("timeout waiting for redis initialization")
}

// isMigrationApplied checks if a migration has been applied
func (r *Runner) isMigrationApplied(ctx context.Context, name string) bool {
	key := fmt.Sprintf("outpost:migration:%s", name)
	val, err := r.client.HGet(ctx, key, "status").Result()
	if err != nil {
		return false
	}
	return val == "applied"
}

// setMigrationApplied marks a migration as applied
func (r *Runner) setMigrationApplied(ctx context.Context, name string) error {
	key := fmt.Sprintf("outpost:migration:%s", name)
	return r.client.HSet(ctx, key,
		"status", "applied",
		"applied_at", time.Now().Format(time.RFC3339),
	).Err()
}

// loggerAdapter adapts logging.Logger to the migratorredis.Logger interface
type loggerAdapter struct {
	logger  *logging.Logger
	verbose bool
}

func (l *loggerAdapter) Verbose() bool {
	return l.verbose
}

func (l *loggerAdapter) LogProgress(current, total int, item string) {
	if l.verbose || current%100 == 0 || current == total {
		l.logger.Debug("migration progress",
			zap.Int("current", current),
			zap.Int("total", total),
			zap.String("item", item))
	}
}

func (l *loggerAdapter) LogInfo(msg string) {
	l.logger.Info(msg)
}

func (l *loggerAdapter) LogDebug(msg string) {
	if l.verbose {
		l.logger.Debug(msg)
	}
}

func (l *loggerAdapter) LogWarning(msg string) {
	l.logger.Warn(msg)
}

func (l *loggerAdapter) LogError(msg string, err error) {
	if err != nil {
		l.logger.Error(msg, zap.Error(err))
	} else {
		l.logger.Error(msg)
	}
}

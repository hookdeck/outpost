package migration_002_timestamps

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	"github.com/hookdeck/outpost/internal/redis"
)

// TimestampsMigration converts timestamp fields (created_at, updated_at)
// from RFC3339 string format to Unix timestamp (int64) for timezone-agnostic sorting.
//
// This migration handles both tenant and destination records.
// It is idempotent - records with numeric timestamps are skipped.
//
// NOTE: disabled_at is intentionally NOT migrated here because:
//   - It's not indexed by RediSearch (not needed for sorting)
//   - Migrating it risks race conditions (user enables destination between Plan/Apply)
//   - Lazy migration handles it: reads accept both formats, and any disable/enable
//     action will write the new Unix format automatically
type TimestampsMigration struct {
	client redis.Client
	logger migratorredis.Logger
}

// timestampUpdates holds the pre-computed updates for Apply phase.
// Key: Redis key, Value: map of field -> Unix timestamp
type timestampUpdates map[string]map[string]int64

// Ensure TimestampsMigration implements the Migration interface
var _ migratorredis.Migration = (*TimestampsMigration)(nil)

// New creates a new TimestampsMigration instance
func New(client redis.Client, logger migratorredis.Logger) *TimestampsMigration {
	return &TimestampsMigration{
		client: client,
		logger: logger,
	}
}

func (m *TimestampsMigration) Name() string {
	return "002_timestamps"
}

func (m *TimestampsMigration) Version() int {
	return 2 // Upgrades schema from v1 to v2
}

func (m *TimestampsMigration) Description() string {
	return "Convert timestamp fields from RFC3339 strings to Unix timestamps for timezone-agnostic sorting"
}

func (m *TimestampsMigration) Plan(ctx context.Context) (*migratorredis.Plan, error) {
	updates := make(timestampUpdates)

	// Collect tenant updates
	m.logger.LogInfo("Scanning tenant records...")
	if err := m.collectUpdates(ctx, "*tenant:*:tenant", []string{"created_at", "updated_at"}, updates); err != nil {
		return nil, fmt.Errorf("failed to scan tenant keys: %w", err)
	}

	// Count tenant stats (keys containing ":tenant" but not ":destination:")
	tenantsNeedMigration := 0
	for key := range updates {
		if strings.Contains(key, ":tenant") && !strings.Contains(key, ":destination:") {
			tenantsNeedMigration++
		}
	}

	// Collect destination updates (same fields as tenant - disabled_at handled by lazy migration)
	m.logger.LogInfo("Scanning destination records...")
	if err := m.collectUpdates(ctx, "*tenant:*:destination:*", []string{"created_at", "updated_at"}, updates); err != nil {
		return nil, fmt.Errorf("failed to scan destination keys: %w", err)
	}

	// Count destination stats
	destsNeedMigration := len(updates) - tenantsNeedMigration

	plan := &migratorredis.Plan{
		MigrationName: m.Name(),
		Description:   m.Description(),
		Version:       "v2",
		Timestamp:     time.Now(),
		Scope: map[string]int{
			"tenants_need_migration":      tenantsNeedMigration,
			"destinations_need_migration": destsNeedMigration,
			"total_need_migration":        len(updates),
		},
		EstimatedItems: len(updates),
		Data:           updates, // Store for Apply phase
	}

	if len(updates) == 0 {
		m.logger.LogInfo("All records already have numeric timestamps")
	} else {
		m.logger.LogInfo(fmt.Sprintf("Found %d records needing timestamp migration (%d tenants, %d destinations)",
			len(updates), tenantsNeedMigration, destsNeedMigration))
	}

	return plan, nil
}

func (m *TimestampsMigration) Apply(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error) {
	state := &migratorredis.State{
		MigrationName: m.Name(),
		Phase:         "applied",
		StartedAt:     time.Now(),
		Progress: migratorredis.Progress{
			TotalItems: plan.EstimatedItems,
		},
		Metadata: make(map[string]interface{}),
	}

	// Get pre-computed updates from Plan phase
	updates, ok := plan.Data.(timestampUpdates)
	if !ok || updates == nil {
		m.logger.LogInfo("No updates to apply")
		completed := time.Now()
		state.CompletedAt = &completed
		return state, nil
	}

	m.logger.LogInfo(fmt.Sprintf("Applying %d timestamp updates...", len(updates)))

	// Batch writes using pipeline
	const batchSize = 100
	pipe := m.client.Pipeline()
	batchCount := 0
	totalProcessed := 0

	for key, fields := range updates {
		// Convert int64 values to interface{} for HSET
		args := make([]interface{}, 0, len(fields)*2)
		for field, value := range fields {
			args = append(args, field, value)
		}
		pipe.HSet(ctx, key, args...)
		batchCount++

		// Execute batch when full
		if batchCount >= batchSize {
			if _, err := pipe.Exec(ctx); err != nil {
				m.logger.LogError("Batch write failed", err)
				state.Progress.FailedItems += batchCount
			} else {
				state.Progress.ProcessedItems += batchCount
			}
			totalProcessed += batchCount
			m.logger.LogProgress(totalProcessed, len(updates), "records")
			pipe = m.client.Pipeline()
			batchCount = 0
		}
	}

	// Execute remaining batch
	if batchCount > 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			m.logger.LogError("Final batch write failed", err)
			state.Progress.FailedItems += batchCount
		} else {
			state.Progress.ProcessedItems += batchCount
		}
		totalProcessed += batchCount
		m.logger.LogProgress(totalProcessed, len(updates), "records")
	}

	completed := time.Now()
	state.CompletedAt = &completed

	return state, nil
}

func (m *TimestampsMigration) Verify(ctx context.Context, state *migratorredis.State) (*migratorredis.VerificationResult, error) {
	result := &migratorredis.VerificationResult{
		Valid:   true,
		Details: make(map[string]string),
	}

	// Check for any remaining unmigrated records
	updates := make(timestampUpdates)

	// Check tenant keys
	if err := m.collectUpdates(ctx, "*tenant:*:tenant", []string{"created_at", "updated_at"}, updates); err != nil {
		return nil, fmt.Errorf("failed to scan tenant keys: %w", err)
	}
	tenantsNeedMigration := len(updates)

	// Check destination keys (disabled_at handled by lazy migration)
	if err := m.collectUpdates(ctx, "*tenant:*:destination:*", []string{"created_at", "updated_at"}, updates); err != nil {
		return nil, fmt.Errorf("failed to scan destination keys: %w", err)
	}
	destsNeedMigration := len(updates) - tenantsNeedMigration

	result.ChecksRun = len(updates)

	if len(updates) > 0 {
		result.Valid = false
		result.Issues = append(result.Issues, fmt.Sprintf("%d records still have RFC3339 timestamps (%d tenants, %d destinations)",
			len(updates), tenantsNeedMigration, destsNeedMigration))
	}

	result.Details["tenants_pending"] = fmt.Sprintf("%d", tenantsNeedMigration)
	result.Details["destinations_pending"] = fmt.Sprintf("%d", destsNeedMigration)

	return result, nil
}

func (m *TimestampsMigration) PlanCleanup(ctx context.Context) (int, error) {
	// No cleanup needed - we're converting in place
	return 0, nil
}

func (m *TimestampsMigration) Cleanup(ctx context.Context, state *migratorredis.State) error {
	// No cleanup needed - timestamps are converted in place
	m.logger.LogInfo("No cleanup needed for timestamps migration")
	return nil
}

// collectUpdates scans keys matching pattern, reads their timestamp fields,
// and collects updates needed (RFC3339 -> Unix conversion).
// Uses SCAN for production safety (non-blocking, cursor-based).
// Works with Redis, Redis Stack, Redis Cluster, and Dragonfly.
func (m *TimestampsMigration) collectUpdates(ctx context.Context, pattern string, fields []string, updates timestampUpdates) error {
	var cursor uint64
	for {
		// SCAN with count hint (doesn't guarantee exact count, just a hint)
		keys, nextCursor, err := m.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}

		// Batch read using pipeline
		if len(keys) > 0 {
			pipe := m.client.Pipeline()
			cmds := make(map[string]*redis.SliceCmd)

			for _, key := range keys {
				// Filter out summary keys
				if strings.Contains(key, ":destinations") && !strings.Contains(key, ":destination:") {
					continue
				}
				cmds[key] = pipe.HMGet(ctx, key, fields...)
			}

			if _, err := pipe.Exec(ctx); err != nil {
				m.logger.LogError("Batch read failed", err)
				// Continue anyway, we'll retry individual keys
			}

			// Process results
			for key, cmd := range cmds {
				data, err := cmd.Result()
				if err != nil {
					m.logger.LogError(fmt.Sprintf("Failed to read %s", key), err)
					continue
				}

				// Check each field and collect updates
				keyUpdates := make(map[string]int64)
				for i, field := range fields {
					if i >= len(data) {
						continue
					}
					value, ok := data[i].(string)
					if !ok || value == "" {
						continue
					}

					// Skip if already numeric
					if _, err := strconv.ParseInt(value, 10, 64); err == nil {
						continue
					}

					// Parse RFC3339 and convert to Unix
					ts, err := parseRFC3339(value)
					if err != nil {
						m.logger.LogError(fmt.Sprintf("Invalid %s in %s: %s", field, key, value), err)
						continue
					}
					keyUpdates[field] = ts.Unix()
				}

				// Only add if there are updates needed
				if len(keyUpdates) > 0 {
					updates[key] = keyUpdates
				}
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return nil
}

// parseRFC3339 parses an RFC3339 timestamp string
func parseRFC3339(value string) (time.Time, error) {
	// Try RFC3339Nano first
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}
	// Fallback to RFC3339
	return time.Parse(time.RFC3339, value)
}

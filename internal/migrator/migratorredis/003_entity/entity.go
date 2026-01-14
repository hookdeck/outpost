package migration_003_entity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	"github.com/hookdeck/outpost/internal/redis"
)

// EntityMigration adds the `entity` field to all tenant and destination records.
// This field is used by RediSearch FILTER to distinguish tenants from destinations
// since both share the same key prefix pattern (tenant:{id}:*).
//
// - Tenants get `entity: "tenant"`
// - Destinations get `entity: "destination"`
//
// This migration is idempotent - records with an existing entity field are skipped.
type EntityMigration struct {
	client    redis.Client
	logger    migratorredis.Logger
	keyPrefix string // deployment prefix for SCAN patterns (empty for single-tenant)
}

// entityUpdates holds the pre-computed updates for Apply phase.
// Key: Redis key, Value: entity type ("tenant" or "destination")
type entityUpdates map[string]string

// Ensure EntityMigration implements the Migration interface
var _ migratorredis.Migration = (*EntityMigration)(nil)

// New creates a new EntityMigration instance.
// deploymentID is optional - pass empty string for single-tenant deployments.
func New(client redis.Client, logger migratorredis.Logger, deploymentID string) *EntityMigration {
	keyPrefix := ""
	if deploymentID != "" {
		keyPrefix = deploymentID + ":"
	}
	return &EntityMigration{
		client:    client,
		logger:    logger,
		keyPrefix: keyPrefix,
	}
}

func (m *EntityMigration) Name() string {
	return "003_entity"
}

func (m *EntityMigration) Version() int {
	return 3 // Upgrades schema from v2 to v3
}

func (m *EntityMigration) Description() string {
	return "Add entity field to tenant and destination records for RediSearch filtering"
}

func (m *EntityMigration) AutoRunnable() bool {
	// NOT auto-runnable - part of v0.12.0 blocking migration.
	// The RediSearch tenant index FILTER requires the entity field to be present,
	// so this migration must run before the new index can be used.
	return false
}

func (m *EntityMigration) Plan(ctx context.Context) (*migratorredis.Plan, error) {
	updates := make(entityUpdates)

	// Build patterns scoped to deployment (or all if no deployment ID)
	tenantPattern := m.keyPrefix + "tenant:*:tenant"
	destPattern := m.keyPrefix + "tenant:*:destination:*"

	// Collect tenant updates
	m.logger.LogInfo("Scanning tenant records...")
	tenantsNeedMigration, err := m.collectUpdates(ctx, tenantPattern, "tenant", updates)
	if err != nil {
		return nil, fmt.Errorf("failed to scan tenant keys: %w", err)
	}

	// Collect destination updates
	m.logger.LogInfo("Scanning destination records...")
	destsNeedMigration, err := m.collectUpdates(ctx, destPattern, "destination", updates)
	if err != nil {
		return nil, fmt.Errorf("failed to scan destination keys: %w", err)
	}

	plan := &migratorredis.Plan{
		MigrationName: m.Name(),
		Description:   m.Description(),
		Version:       "v3",
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
		m.logger.LogInfo("All records already have entity field")
	} else {
		m.logger.LogInfo(fmt.Sprintf("Found %d records needing entity field (%d tenants, %d destinations)",
			len(updates), tenantsNeedMigration, destsNeedMigration))
	}

	return plan, nil
}

func (m *EntityMigration) Apply(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error) {
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
	updates, ok := plan.Data.(entityUpdates)
	if !ok || updates == nil {
		m.logger.LogInfo("No updates to apply")
		completed := time.Now()
		state.CompletedAt = &completed
		return state, nil
	}

	m.logger.LogInfo(fmt.Sprintf("Applying %d entity field updates...", len(updates)))

	// Batch writes using pipeline
	const batchSize = 100
	pipe := m.client.Pipeline()
	batchCount := 0
	totalProcessed := 0

	for key, entityType := range updates {
		pipe.HSet(ctx, key, "entity", entityType)
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

func (m *EntityMigration) Verify(ctx context.Context, state *migratorredis.State) (*migratorredis.VerificationResult, error) {
	result := &migratorredis.VerificationResult{
		Valid:   true,
		Details: make(map[string]string),
	}

	// Build patterns scoped to deployment (or all if no deployment ID)
	tenantPattern := m.keyPrefix + "tenant:*:tenant"
	destPattern := m.keyPrefix + "tenant:*:destination:*"

	// Check for any remaining unmigrated records
	updates := make(entityUpdates)

	// Check tenant keys
	tenantsNeedMigration, err := m.collectUpdates(ctx, tenantPattern, "tenant", updates)
	if err != nil {
		return nil, fmt.Errorf("failed to scan tenant keys: %w", err)
	}

	// Check destination keys
	destsNeedMigration, err := m.collectUpdates(ctx, destPattern, "destination", updates)
	if err != nil {
		return nil, fmt.Errorf("failed to scan destination keys: %w", err)
	}

	result.ChecksRun = len(updates)

	if len(updates) > 0 {
		result.Valid = false
		result.Issues = append(result.Issues, fmt.Sprintf("%d records still missing entity field (%d tenants, %d destinations)",
			len(updates), tenantsNeedMigration, destsNeedMigration))
	}

	result.Details["tenants_pending"] = fmt.Sprintf("%d", tenantsNeedMigration)
	result.Details["destinations_pending"] = fmt.Sprintf("%d", destsNeedMigration)

	return result, nil
}

func (m *EntityMigration) PlanCleanup(ctx context.Context) (int, error) {
	// No cleanup needed - we're adding a field in place
	return 0, nil
}

func (m *EntityMigration) Cleanup(ctx context.Context, state *migratorredis.State) error {
	// No cleanup needed - entity field is added in place
	m.logger.LogInfo("No cleanup needed for entity migration")
	return nil
}

// collectUpdates scans keys matching pattern, checks if they have the entity field,
// and collects updates needed. Returns count of records needing migration.
func (m *EntityMigration) collectUpdates(ctx context.Context, pattern string, entityType string, updates entityUpdates) (int, error) {
	count := 0
	var cursor uint64
	for {
		// SCAN with count hint (doesn't guarantee exact count, just a hint)
		keys, nextCursor, err := m.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return count, fmt.Errorf("scan failed: %w", err)
		}

		// Batch read using pipeline
		if len(keys) > 0 {
			pipe := m.client.Pipeline()
			cmds := make(map[string]*redis.StringCmd)

			for _, key := range keys {
				// Filter out summary keys
				if strings.Contains(key, ":destinations") && !strings.Contains(key, ":destination:") {
					continue
				}
				cmds[key] = pipe.HGet(ctx, key, "entity")
			}

			if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
				// Ignore nil errors - they just mean the field doesn't exist
				m.logger.LogError("Batch read failed", err)
			}

			// Process results
			for key, cmd := range cmds {
				value, err := cmd.Result()
				if err == redis.Nil || value == "" {
					// Field doesn't exist - needs migration
					updates[key] = entityType
					count++
				} else if err != nil {
					m.logger.LogError(fmt.Sprintf("Failed to read entity field from %s", key), err)
				}
				// If value exists and matches expected, skip (already migrated)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return count, nil
}

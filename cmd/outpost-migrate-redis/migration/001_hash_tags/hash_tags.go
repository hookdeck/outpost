package migration_001_hash_tags

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hookdeck/outpost/cmd/outpost-migrate-redis/migration"
	"github.com/hookdeck/outpost/internal/redis"
)

// HashTagsMigration migrates from legacy format (tenant:*) to hash-tagged format ({tenant}:*)
type HashTagsMigration struct {
	client redis.Client
	logger migration.Logger
}

// Ensure HashTagsMigration implements the Migration interface
var _ migration.Migration = (*HashTagsMigration)(nil)

// New creates a new HashTagsMigration instance
func New(client redis.Client, logger migration.Logger) *HashTagsMigration {
	return &HashTagsMigration{
		client: client,
		logger: logger,
	}
}

func (m *HashTagsMigration) Name() string {
	return "001_hash_tags"
}

func (m *HashTagsMigration) Version() int {
	return 1 // Upgrades schema from v0 to v1
}

func (m *HashTagsMigration) Description() string {
	return "Migrate from legacy format (tenant:*) to hash-tagged format (tenant:{id}:*) for Redis Cluster support"
}

func (m *HashTagsMigration) Plan(ctx context.Context) (*migration.Plan, error) {
	// Find all legacy tenants
	legacyKeys, err := m.client.Keys(ctx, "tenant:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to scan keys: %w", err)
	}

	// Categorize keys
	tenants := make(map[string]bool)
	destinationSummaryCount := 0
	individualDestinationCount := 0

	for _, key := range legacyKeys {
		parts := strings.Split(key, ":")
		if len(parts) >= 2 && parts[0] == "tenant" {
			tenantID := parts[1]

			// Track unique tenants
			if len(parts) == 2 {
				tenants[tenantID] = true
			} else if len(parts) == 3 && parts[2] == "destinations" {
				// This is a destination summary key: tenant:ID:destinations
				destinationSummaryCount++
			} else if len(parts) == 4 && parts[2] == "destination" {
				// This is an individual destination key: tenant:ID:destination:DEST_ID
				individualDestinationCount++
			}
		}
	}

	if len(tenants) == 0 {
		return &migration.Plan{
			MigrationName:  m.Name(),
			Description:    m.Description(),
			Version:        "v2",
			Timestamp:      time.Now(),
			Scope:          map[string]int{"tenants": 0, "dest_summaries": 0, "destinations": 0, "total_keys": 0},
			EstimatedItems: 0,
		}, nil
	}

	// Build tenant list
	tenantList := make([]string, 0, len(tenants))
	for id := range tenants {
		tenantList = append(tenantList, id)
	}

	plan := &migration.Plan{
		MigrationName: m.Name(),
		Description:   m.Description(),
		Version:       "v2",
		Timestamp:     time.Now(),
		Scope: map[string]int{
			"tenants":        len(tenants),
			"dest_summaries": destinationSummaryCount,
			"destinations":   individualDestinationCount,
			"total_keys":     len(legacyKeys),
		},
		EstimatedItems: len(legacyKeys),
		Metadata: map[string]string{
			"tenant_ids": strings.Join(tenantList[:min(10, len(tenantList))], ","),
		},
	}

	m.logger.LogInfo(fmt.Sprintf("Found %d tenants to migrate", len(tenants)))
	if m.logger.Verbose() {
		if len(tenantList) > 10 {
			m.logger.LogDebug(fmt.Sprintf("First 10 tenants: %v ... and %d more", tenantList[:10], len(tenantList)-10))
		} else if len(tenantList) > 0 {
			m.logger.LogDebug(fmt.Sprintf("Tenants: %v", tenantList))
		}
	}

	return plan, nil
}

func (m *HashTagsMigration) Apply(ctx context.Context, plan *migration.Plan) (*migration.State, error) {
	state := &migration.State{
		MigrationName: m.Name(),
		Phase:         "applied",
		StartedAt:     time.Now(),
		Progress: migration.Progress{
			TotalItems: plan.Scope["tenants"],
		},
		Metadata: make(map[string]interface{}),
	}

	// Get all tenant IDs from legacy keys
	legacyKeys, err := m.client.Keys(ctx, "tenant:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to scan keys: %w", err)
	}

	// Extract unique tenant IDs
	tenants := make(map[string]bool)
	for _, key := range legacyKeys {
		parts := strings.Split(key, ":")
		if len(parts) >= 2 && parts[0] == "tenant" && len(parts) == 2 {
			tenants[parts[1]] = true
		}
	}

	// Migrate each tenant
	i := 0
	for tenantID := range tenants {
		i++
		m.logger.LogProgress(i, len(tenants), tenantID)

		if err := m.migrateTenant(ctx, tenantID); err != nil {
			m.logger.LogError(fmt.Sprintf("Failed to migrate tenant %s", tenantID), err)
			state.Errors = append(state.Errors, fmt.Sprintf("tenant %s: %v", tenantID, err))
			state.Progress.FailedItems++
			continue
		}

		state.Progress.ProcessedItems++

		if m.logger.Verbose() {
			m.logger.LogDebug(fmt.Sprintf("Migrated tenant: %s", tenantID))
		}
	}

	// Store only counts in metadata (not tenant IDs)
	state.Metadata["total_tenants"] = len(tenants)

	completed := time.Now()
	state.CompletedAt = &completed

	return state, nil
}

func (m *HashTagsMigration) Verify(ctx context.Context, state *migration.State) (*migration.VerificationResult, error) {
	result := &migration.VerificationResult{
		Valid:   true,
		Details: make(map[string]string),
	}

	// Get all legacy tenant keys for spot checking
	legacyKeys, err := m.client.Keys(ctx, "tenant:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to scan keys for verification: %w", err)
	}

	// Extract tenant IDs from legacy keys
	tenants := []string{}
	for _, key := range legacyKeys {
		parts := strings.Split(key, ":")
		if len(parts) == 2 && parts[0] == "tenant" {
			tenants = append(tenants, parts[1])
		}
	}

	if len(tenants) == 0 {
		result.Details["status"] = "No tenants to verify"
		return result, nil
	}

	// Determine sample size for spot checking
	sampleSize := 20 // Check up to 20 tenants
	if len(tenants) < sampleSize {
		sampleSize = len(tenants) // Check all if less than 20
	}

	m.logger.LogInfo(fmt.Sprintf("Spot checking %d out of %d tenants...", sampleSize, len(tenants)))

	// Randomly sample tenants to verify
	// Simple approach: just take first N tenants (could randomize if needed)
	for i := 0; i < sampleSize; i++ {
		tenantID := tenants[i]
		result.ChecksRun++

		// 1. Check tenant key
		tenantPassed := true
		newTenantKey := fmt.Sprintf("tenant:{%s}:tenant", tenantID)
		exists, err := m.client.Exists(ctx, newTenantKey).Result()
		if err != nil || exists == 0 {
			result.Valid = false
			result.Issues = append(result.Issues, fmt.Sprintf("Missing new tenant key: %s", newTenantKey))
			tenantPassed = false
		} else {
			// Verify tenant data integrity
			oldData, _ := m.client.HGetAll(ctx, fmt.Sprintf("tenant:%s", tenantID)).Result()
			newData, _ := m.client.HGetAll(ctx, newTenantKey).Result()

			if len(oldData) != len(newData) {
				result.Issues = append(result.Issues,
					fmt.Sprintf("Tenant data mismatch for %s: old has %d fields, new has %d",
						tenantID, len(oldData), len(newData)))
				tenantPassed = false
			} else if m.logger.Verbose() {
				// Deep check: compare actual values
				for field, oldValue := range oldData {
					if newValue, ok := newData[field]; !ok || newValue != oldValue {
						result.Issues = append(result.Issues,
							fmt.Sprintf("Field mismatch for tenant %s, field %s", tenantID, field))
						tenantPassed = false
						break
					}
				}
			}
		}

		// 2. Check destinations summary
		destSummaryPassed := true
		oldDestSummaryKey := fmt.Sprintf("tenant:%s:destinations", tenantID)
		newDestSummaryKey := fmt.Sprintf("tenant:{%s}:destinations", tenantID)

		oldDestSummary, _ := m.client.HGetAll(ctx, oldDestSummaryKey).Result()
		if len(oldDestSummary) > 0 {
			exists, err := m.client.Exists(ctx, newDestSummaryKey).Result()
			if err != nil || exists == 0 {
				result.Issues = append(result.Issues, fmt.Sprintf("Missing new destinations summary: %s", newDestSummaryKey))
				destSummaryPassed = false
			} else {
				newDestSummary, _ := m.client.HGetAll(ctx, newDestSummaryKey).Result()
				if len(oldDestSummary) != len(newDestSummary) {
					result.Issues = append(result.Issues,
						fmt.Sprintf("Destinations summary mismatch for %s: old has %d, new has %d",
							tenantID, len(oldDestSummary), len(newDestSummary)))
					destSummaryPassed = false
				}
			}
		}

		// 3. Check individual destinations
		destsPassed := true
		destIDs, _ := m.client.HKeys(ctx, oldDestSummaryKey).Result()
		for _, destID := range destIDs {
			oldDestKey := fmt.Sprintf("tenant:%s:destination:%s", tenantID, destID)
			newDestKey := fmt.Sprintf("tenant:{%s}:destination:%s", tenantID, destID)

			oldDestData, _ := m.client.HGetAll(ctx, oldDestKey).Result()
			if len(oldDestData) > 0 {
				exists, err := m.client.Exists(ctx, newDestKey).Result()
				if err != nil || exists == 0 {
					result.Issues = append(result.Issues, fmt.Sprintf("Missing destination: %s", newDestKey))
					destsPassed = false
				} else if m.logger.Verbose() {
					newDestData, _ := m.client.HGetAll(ctx, newDestKey).Result()
					if len(oldDestData) != len(newDestData) {
						result.Issues = append(result.Issues,
							fmt.Sprintf("Destination %s data mismatch: old has %d fields, new has %d",
								destID, len(oldDestData), len(newDestData)))
						destsPassed = false
					}
				}
			}
		}

		// Count as passed only if all checks passed
		if tenantPassed && destSummaryPassed && destsPassed {
			result.ChecksPassed++
		}
	}

	result.Details["total_tenants"] = fmt.Sprintf("%d", len(tenants))
	result.Details["tenants_checked"] = fmt.Sprintf("%d", sampleSize)
	result.Details["checks_passed"] = fmt.Sprintf("%d/%d", result.ChecksPassed, result.ChecksRun)

	return result, nil
}

// getLegacyKeys returns all legacy keys (those without hash tags)
func (m *HashTagsMigration) getLegacyKeys(ctx context.Context) ([]string, error) {
	// Get all keys matching tenant:* pattern
	allKeys, err := m.client.Keys(ctx, "tenant:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to scan keys: %w", err)
	}

	// Filter to only legacy keys (those WITHOUT hash tags)
	var legacyKeys []string
	for _, key := range allKeys {
		// New keys have hash tags like tenant:{123}:...
		// Old keys don't have curly braces
		if !strings.Contains(key, "{") && !strings.Contains(key, "}") {
			legacyKeys = append(legacyKeys, key)
		}
	}

	return legacyKeys, nil
}

// PlanCleanup analyzes what would be cleaned up without making changes
func (m *HashTagsMigration) PlanCleanup(ctx context.Context) (int, error) {
	legacyKeys, err := m.getLegacyKeys(ctx)
	if err != nil {
		return 0, err
	}
	return len(legacyKeys), nil
}

func (m *HashTagsMigration) Cleanup(ctx context.Context, state *migration.State) error {
	// Get legacy keys to clean up
	legacyKeys, err := m.getLegacyKeys(ctx)
	if err != nil {
		return err
	}

	if len(legacyKeys) == 0 {
		m.logger.LogInfo("No legacy keys to cleanup.")
		return nil
	}

	m.logger.LogInfo(fmt.Sprintf("Found %d legacy keys to remove.", len(legacyKeys)))

	// Delete in batches
	batchSize := 100
	deleted := 0

	for i := 0; i < len(legacyKeys); i += batchSize {
		end := min(i+batchSize, len(legacyKeys))
		batch := legacyKeys[i:end]

		if err := m.client.Del(ctx, batch...).Err(); err != nil {
			return fmt.Errorf("failed to delete batch: %w", err)
		}

		deleted += len(batch)
		if deleted%500 == 0 || deleted == len(legacyKeys) {
			m.logger.LogProgress(deleted, len(legacyKeys), "keys")
		}
	}

	m.logger.LogInfo(fmt.Sprintf("Cleanup complete! Removed %d legacy keys.", deleted))
	return nil
}

func (m *HashTagsMigration) migrateTenant(ctx context.Context, tenantID string) error {
	// Use transaction for atomic migration
	pipe := m.client.TxPipeline()

	// Migrate tenant data
	oldTenantKey := fmt.Sprintf("tenant:%s", tenantID)
	newTenantKey := fmt.Sprintf("tenant:{%s}:tenant", tenantID)

	tenantData, err := m.client.HGetAll(ctx, oldTenantKey).Result()
	if err == nil && len(tenantData) > 0 {
		pipe.HMSet(ctx, newTenantKey, tenantData)
	}

	// Migrate destinations summary
	oldDestKey := fmt.Sprintf("tenant:%s:destinations", tenantID)
	newDestKey := fmt.Sprintf("tenant:{%s}:destinations", tenantID)

	destData, err := m.client.HGetAll(ctx, oldDestKey).Result()
	if err == nil && len(destData) > 0 {
		pipe.HMSet(ctx, newDestKey, destData)

		// Migrate individual destinations
		destIDs, _ := m.client.HKeys(ctx, oldDestKey).Result()
		for _, destID := range destIDs {
			oldKey := fmt.Sprintf("tenant:%s:destination:%s", tenantID, destID)
			newKey := fmt.Sprintf("tenant:{%s}:destination:%s", tenantID, destID)

			data, err := m.client.HGetAll(ctx, oldKey).Result()
			if err == nil && len(data) > 0 {
				pipe.HMSet(ctx, newKey, data)
			}
		}
	}

	// Execute transaction
	_, err = pipe.Exec(ctx)
	return err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

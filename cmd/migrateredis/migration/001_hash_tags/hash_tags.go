package migration_001_hash_tags

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hookdeck/outpost/cmd/migrateredis/migration"
	"github.com/hookdeck/outpost/internal/redis"
)

// HashTagsMigration migrates from legacy format (tenant:*) to hash-tagged format ({tenant}:*)
type HashTagsMigration struct{}

// Ensure HashTagsMigration implements the Migration interface
var _ migration.Migration = (*HashTagsMigration)(nil)

func New() migration.Migration {
	return &HashTagsMigration{}
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

func (m *HashTagsMigration) Plan(ctx context.Context, client redis.Client, verbose bool) (*migration.Plan, error) {
	// Find all legacy tenants
	legacyKeys, err := client.Keys(ctx, "tenant:*").Result()
	if err != nil {
		return nil, fmt.Errorf("failed to scan keys: %w", err)
	}

	// Categorize keys
	tenants := make(map[string]bool)
	destinationCount := 0

	for _, key := range legacyKeys {
		parts := strings.Split(key, ":")
		if len(parts) >= 2 && parts[0] == "tenant" {
			tenantID := parts[1]

			// Track unique tenants
			if len(parts) == 2 {
				tenants[tenantID] = true
			} else if strings.Contains(key, ":destinations") || strings.Contains(key, ":destination:") {
				destinationCount++
			}
		}
	}

	if len(tenants) == 0 {
		return &migration.Plan{
			MigrationName:  m.Name(),
			Description:    m.Description(),
			Version:        "v2",
			Timestamp:      time.Now(),
			Scope:          map[string]int{"tenants": 0, "destinations": 0, "total_keys": 0},
			EstimatedItems: 0,
			EstimatedTime:  "0s",
		}, nil
	}

	// Build tenant list
	tenantList := make([]string, 0, len(tenants))
	for id := range tenants {
		tenantList = append(tenantList, id)
	}

	plan := &migration.Plan{
		MigrationName:  m.Name(),
		Description:    m.Description(),
		Version:        "v2",
		Timestamp:      time.Now(),
		Scope: map[string]int{
			"tenants":      len(tenants),
			"destinations": destinationCount,
			"total_keys":   len(legacyKeys),
		},
		EstimatedItems: len(legacyKeys),
		EstimatedTime:  fmt.Sprintf("~%d seconds", len(tenants)/10+1),
		Metadata: map[string]string{
			"tenant_ids": strings.Join(tenantList[:min(10, len(tenantList))], ","),
		},
	}

	if verbose {
		fmt.Printf("Found %d tenants to migrate:\n", len(tenants))
		for i, tenant := range tenantList {
			if i >= 10 {
				fmt.Printf("  ... and %d more\n", len(tenantList)-10)
				break
			}
			fmt.Printf("  - %s\n", tenant)
		}
	}

	return plan, nil
}

func (m *HashTagsMigration) Apply(ctx context.Context, client redis.Client, plan *migration.Plan, verbose bool) (*migration.State, error) {
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
	legacyKeys, err := client.Keys(ctx, "tenant:*").Result()
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
		if !verbose && i%10 == 0 {
			fmt.Printf("Progress: %d/%d tenants\n", i, len(tenants))
		}

		if err := m.migrateTenant(ctx, client, tenantID); err != nil {
			if verbose {
				fmt.Printf("❌ Failed to migrate tenant %s: %v\n", tenantID, err)
			}
			state.Errors = append(state.Errors, fmt.Sprintf("tenant %s: %v", tenantID, err))
			state.Progress.FailedItems++
			continue
		}

		state.Progress.ProcessedItems++

		if verbose {
			fmt.Printf("  ✓ Migrated tenant: %s\n", tenantID)
		}
	}

	// Store only counts in metadata (not tenant IDs)
	state.Metadata["total_tenants"] = len(tenants)

	completed := time.Now()
	state.CompletedAt = &completed

	return state, nil
}

func (m *HashTagsMigration) Verify(ctx context.Context, client redis.Client, state *migration.State, verbose bool) (*migration.VerificationResult, error) {
	result := &migration.VerificationResult{
		Valid:    true,
		Details:  make(map[string]string),
	}

	// Get all legacy tenant keys for spot checking
	legacyKeys, err := client.Keys(ctx, "tenant:*").Result()
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

	fmt.Printf("Spot checking %d out of %d tenants...\n", sampleSize, len(tenants))

	// Randomly sample tenants to verify
	// Simple approach: just take first N tenants (could randomize if needed)
	for i := 0; i < sampleSize; i++ {
		tenantID := tenants[i]
		result.ChecksRun++

		// Check if new key exists
		newKey := fmt.Sprintf("tenant:{%s}", tenantID)
		exists, err := client.Exists(ctx, newKey).Result()
		if err != nil || exists == 0 {
			result.Valid = false
			result.Issues = append(result.Issues, fmt.Sprintf("Missing new key for tenant: %s", tenantID))
			continue
		}

		// Verify data integrity
		oldData, _ := client.HGetAll(ctx, fmt.Sprintf("tenant:%s", tenantID)).Result()
		newData, _ := client.HGetAll(ctx, newKey).Result()

		if len(oldData) != len(newData) {
			result.Issues = append(result.Issues,
				fmt.Sprintf("Data mismatch for tenant %s: old has %d fields, new has %d",
					tenantID, len(oldData), len(newData)))
		} else {
			// Deep check: compare actual values if verbose
			if verbose {
				mismatch := false
				for field, oldValue := range oldData {
					if newValue, ok := newData[field]; !ok || newValue != oldValue {
						mismatch = true
						result.Issues = append(result.Issues,
							fmt.Sprintf("Field mismatch for tenant %s, field %s", tenantID, field))
						break
					}
				}
				if !mismatch {
					result.ChecksPassed++
				}
			} else {
				result.ChecksPassed++
			}
		}
	}

	result.Details["total_tenants"] = fmt.Sprintf("%d", len(tenants))
	result.Details["tenants_checked"] = fmt.Sprintf("%d", sampleSize)
	result.Details["checks_passed"] = fmt.Sprintf("%d/%d", result.ChecksPassed, result.ChecksRun)

	return result, nil
}

func (m *HashTagsMigration) Cleanup(ctx context.Context, client redis.Client, state *migration.State, force bool, verbose bool) error {
	// Get all legacy keys
	legacyKeys, err := client.Keys(ctx, "tenant:*").Result()
	if err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	if len(legacyKeys) == 0 {
		fmt.Println("No legacy keys to cleanup.")
		return nil
	}

	fmt.Printf("Found %d legacy keys to remove.\n", len(legacyKeys))

	if !force {
		fmt.Printf("⚠️  WARNING: This will permanently delete %d legacy keys.\n", len(legacyKeys))
		fmt.Printf("Continue? (y/N): ")

		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Cleanup cancelled.")
			return fmt.Errorf("cleanup cancelled by user")
		}
	}

	// Delete in batches
	batchSize := 100
	deleted := 0

	for i := 0; i < len(legacyKeys); i += batchSize {
		end := min(i+batchSize, len(legacyKeys))
		batch := legacyKeys[i:end]

		if err := client.Del(ctx, batch...).Err(); err != nil {
			return fmt.Errorf("failed to delete batch: %w", err)
		}

		deleted += len(batch)
		if !verbose && deleted%500 == 0 {
			fmt.Printf("Progress: %d/%d keys deleted\n", deleted, len(legacyKeys))
		}
	}

	fmt.Printf("✅ Cleanup complete! Removed %d legacy keys.\n", deleted)
	return nil
}

func (m *HashTagsMigration) migrateTenant(ctx context.Context, client redis.Client, tenantID string) error {
	// Use transaction for atomic migration
	pipe := client.TxPipeline()

	// Migrate tenant data
	oldTenantKey := fmt.Sprintf("tenant:%s", tenantID)
	newTenantKey := fmt.Sprintf("tenant:{%s}", tenantID)

	tenantData, err := client.HGetAll(ctx, oldTenantKey).Result()
	if err == nil && len(tenantData) > 0 {
		pipe.HMSet(ctx, newTenantKey, tenantData)
	}

	// Migrate destinations summary
	oldDestKey := fmt.Sprintf("tenant:%s:destinations", tenantID)
	newDestKey := fmt.Sprintf("tenant:{%s}:destinations", tenantID)

	destData, err := client.HGetAll(ctx, oldDestKey).Result()
	if err == nil && len(destData) > 0 {
		pipe.HMSet(ctx, newDestKey, destData)

		// Migrate individual destinations
		destIDs, _ := client.HKeys(ctx, oldDestKey).Result()
		for _, destID := range destIDs {
			oldKey := fmt.Sprintf("tenant:%s:destination:%s", tenantID, destID)
			newKey := fmt.Sprintf("tenant:{%s}:destination:%s", tenantID, destID)

			data, err := client.HGetAll(ctx, oldKey).Result()
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
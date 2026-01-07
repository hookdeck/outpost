package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/hookdeck/outpost/cmd/outpost-migrate-redis/migration"
	migration_001 "github.com/hookdeck/outpost/cmd/outpost-migrate-redis/migration/001_hash_tags"
	migration_002 "github.com/hookdeck/outpost/cmd/outpost-migrate-redis/migration/002_timestamps"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/redis"
)

const (
	migrationLockKey = ".outpost:migration:lock"
)

// Migrator handles Redis migrations
type Migrator struct {
	client     *redisClientWrapper
	logger     MigrationLogger
	migrations map[string]migration.Migration // All available migrations
}

// Close cleans up resources (logger sync, redis connection, etc)
func (m *Migrator) Close() error {
	var lastErr error

	// Sync logger if it implements Sync
	if syncer, ok := m.logger.(interface{ Sync() error }); ok {
		if err := syncer.Sync(); err != nil {
			lastErr = fmt.Errorf("failed to sync logger: %w", err)
		}
	}

	// Close Redis client if it implements Close
	// (for future when we might need to close connections)
	if closer, ok := m.client.Cmdable.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			lastErr = fmt.Errorf("failed to close redis client: %w", err)
		}
	}

	return lastErr
}

// NewMigrator creates a new migrator instance
func NewMigrator(cfg *config.Config, logger MigrationLogger) (*Migrator, error) {
	ctx := context.Background()

	// Build Redis client
	redisConfig := cfg.Redis.ToConfig()
	redisClient, err := redis.New(ctx, redisConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Create wrapper client
	client := &redisClientWrapper{
		Cmdable: redisClient,
	}

	// Initialize all migrations
	migrations := make(map[string]migration.Migration)

	// Helper to register a migration by its name
	registerMigration := func(m migration.Migration) {
		migrations[m.Name()] = m
	}

	// Register all migrations
	registerMigration(migration_001.New(client, logger))
	registerMigration(migration_002.New(client, logger))

	return &Migrator{
		client:     client,
		logger:     logger,
		migrations: migrations,
	}, nil
}

// ListMigrations lists all available migrations
func (m *Migrator) ListMigrations() error {
	// Build map of name -> description from actual migrations
	migrationMap := make(map[string]string)
	for name, mig := range m.migrations {
		migrationMap[name] = mig.Description()
	}

	m.logger.LogMigrationList(migrationMap)
	return nil
}

// acquireLock attempts to acquire a lock for running migrations
func (m *Migrator) acquireLock(ctx context.Context, migrationName string) error {
	// Create lock with details
	lock := fmt.Sprintf("migration=%s, started=%s", migrationName, time.Now().Format(time.RFC3339))

	// Try to set lock atomically with SetNX (only sets if not exists)
	// Use 1 hour expiry in case process dies without cleanup
	success, err := m.client.SetNX(ctx, migrationLockKey, lock, time.Hour).Result()
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !success {
		// Lock already exists, get details for error message
		lockData, err := m.client.Get(ctx, migrationLockKey).Result()
		if err != nil {
			return fmt.Errorf("migration is already running (could not get lock details: %w)", err)
		}

		return fmt.Errorf("migration is already running: %s\n"+
			"If this is a stale lock, run: migrateredis unlock", lockData)
	}

	return nil
}

// releaseLock releases the migration lock
func (m *Migrator) releaseLock(ctx context.Context) error {
	err := m.client.Del(ctx, migrationLockKey).Err()
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}
	m.logger.LogLockReleased()
	return nil
}

// Unlock forcefully clears the migration lock (for stuck situations)
func (m *Migrator) Unlock(ctx context.Context, autoApprove bool) error {
	// Check if lock exists
	lockData, err := m.client.Get(ctx, migrationLockKey).Result()
	if err != nil && err.Error() == "redis: nil" {
		m.logger.LogLockStatus("", false)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get lock details: %w", err)
	}

	m.logger.LogLockStatus(lockData, true)

	if !autoApprove {
		confirmed, err := m.logger.ConfirmWithWarning(
			"Clearing a lock while a migration is running could cause issues. Only clear if you're certain the migration is not running.",
			"Continue?",
		)
		if err != nil {
			return err
		}
		if !confirmed {
			m.logger.LogInfo("lock clear cancelled")
			return nil
		}
	}

	err = m.client.Del(ctx, migrationLockKey).Err()
	if err != nil {
		return fmt.Errorf("failed to clear lock: %w", err)
	}

	m.logger.LogLockCleared()
	return nil
}

// Init handles initialization for fresh installations
func (m *Migrator) Init(ctx context.Context, currentCheck bool) error {
	m.logger.LogCheckingInstallation()

	// Check if Redis has any existing data first
	isFresh, err := m.checkIfFreshInstallation(ctx)
	if err != nil {
		return fmt.Errorf("failed to check installation status: %w", err)
	}

	if isFresh {
		m.logger.LogFreshInstallation()

		// Try to acquire lock - this is atomic with SetNX
		err := m.acquireLock(ctx, "init")
		if err != nil {
			// Someone else is initializing - wait for them
			m.logger.LogLockWaiting()

			// Wait for initialization to complete (check every second, max 30 seconds)
			for i := 0; i < 30; i++ {
				time.Sleep(1 * time.Second)

				stillFresh, err := m.checkIfFreshInstallation(ctx)
				if err != nil {
					return fmt.Errorf("failed to check installation status: %w", err)
				}

				if !stillFresh {
					m.logger.LogInitialization(false, 0)
					return nil
				}
			}

			return fmt.Errorf("timeout waiting for Redis initialization")
		}

		// We got the lock
		m.logger.LogLockAcquired()
		defer m.releaseLock(ctx)

		isFresh, err = m.checkIfFreshInstallation(ctx)
		if err != nil {
			return fmt.Errorf("failed to recheck installation status: %w", err)
		}

		if !isFresh {
			// Someone else initialized while we were acquiring lock
			m.logger.LogInitialization(false, 0)
			return nil
		}

		// Still fresh and we have the lock - do the initialization
		m.logger.LogInfo("initializing redis")

		// Mark all migrations as applied
		for name := range m.migrations {
			if err := setMigrationAsApplied(ctx, m.client, name); err != nil {
				return fmt.Errorf("failed to mark migration %s as applied: %w", name, err)
			}
		}

		m.logger.LogInitialization(true, len(m.migrations))
		return nil
	}

	// Not fresh - Redis already initialized (no lock needed)
	m.logger.LogExistingInstallation()

	// If --current flag is set, check if migrations are pending
	if currentCheck {
		// Get pending migrations count
		pendingCount := 0
		for name := range m.migrations {
			if !isApplied(ctx, m.client, name) {
				pendingCount++
			}
		}

		if pendingCount > 0 {
			m.logger.LogPendingMigrations(pendingCount)
			os.Exit(1)
		}
		// Up to date - exit normally
	}

	return nil
}

// checkIfFreshInstallation checks if Redis is a fresh installation
func (m *Migrator) checkIfFreshInstallation(ctx context.Context) (bool, error) {
	// Check for any "outpost:*" keys (current format)
	outpostKeys, err := m.client.Keys(ctx, "outpost:*").Result()
	if err != nil {
		return false, fmt.Errorf("failed to check outpost keys: %w", err)
	}
	if len(outpostKeys) > 0 {
		return false, nil // Has current data
	}

	// Check for any "tenant:*" keys (old format)
	tenantKeys, err := m.client.Keys(ctx, "tenant:*").Result()
	if err != nil {
		return false, fmt.Errorf("failed to check tenant keys: %w", err)
	}
	if len(tenantKeys) > 0 {
		return false, nil // Has old data
	}

	// No keys found - it's a fresh installation
	return true, nil
}

// Plan shows what changes would be made without applying them
func (m *Migrator) Plan(ctx context.Context) error {
	// First show current status
	var appliedCount, pendingCount int

	for name := range m.migrations {
		if isApplied(ctx, m.client, name) {
			appliedCount++
		} else {
			pendingCount++
		}
	}

	m.logger.LogMigrationStatus(appliedCount, pendingCount)

	if pendingCount == 0 {
		m.logger.LogNoMigrationsNeeded()
		return nil
	}

	// Get the next unapplied migration
	mig, err := m.getNextMigration(ctx)
	if err != nil {
		return err
	}

	// Run the migration in plan mode
	plan, err := mig.Plan(ctx)
	if err != nil {
		return err
	}

	// Display the plan
	m.logger.LogMigrationPlan(mig.Name(), plan)

	return nil
}

// Verify verifies that a migration was successful
func (m *Migrator) Verify(ctx context.Context) error {
	// Get the last applied migration
	mig, err := m.getLastAppliedMigration(ctx)
	if err != nil {
		return err
	}

	m.logger.LogVerificationStart(mig.Name())

	// Run verification
	// Note: We're passing a minimal state object since the full state isn't stored yet
	verifyState := &migration.State{
		MigrationName: mig.Name(),
		Phase:         "applied",
	}

	result, err := mig.Verify(ctx, verifyState)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	// Display results
	m.logger.LogVerificationResult(mig.Name(), result)

	if !result.Valid {
		return fmt.Errorf("migration verification failed")
	}

	return nil
}

// Cleanup removes old keys after successful migration
func (m *Migrator) Cleanup(ctx context.Context, force, autoApprove bool) error {
	// Get the last applied migration
	mig, err := m.getLastAppliedMigration(ctx)
	if err != nil {
		return err
	}

	// First verify the migration if not forced
	if !force {
		m.logger.LogInfo("verifying migration before cleanup")
		verifyState := &migration.State{
			MigrationName: mig.Name(),
			Phase:         "applied",
		}

		result, err := mig.Verify(ctx, verifyState)
		if err != nil {
			return fmt.Errorf("verification failed: %w", err)
		}

		if !result.Valid {
			return fmt.Errorf("migration verification failed - cleanup aborted. Use --force to override")
		}
		m.logger.LogInfo("verification passed")
	}

	m.logger.LogCleanupAnalysis(0) // Initial message

	// Plan what would be cleaned up
	legacyKeyCount, err := mig.PlanCleanup(ctx)
	if err != nil {
		return fmt.Errorf("failed to analyze cleanup scope: %w", err)
	}

	if legacyKeyCount == 0 {
		m.logger.LogNoCleanupNeeded()
		return nil
	}

	// Confirm if not auto-approved
	if !autoApprove && !force {
		confirmed, err := m.logger.ConfirmWithWarning(
			fmt.Sprintf("This will delete approximately %d old Redis keys. This action cannot be undone.", legacyKeyCount),
			"Do you want to continue?",
		)
		if err != nil {
			return err
		}
		if !confirmed {
			m.logger.LogInfo("cleanup cancelled")
			return nil
		}
	}

	// Acquire lock before cleanup
	m.logger.LogLockAcquiring(fmt.Sprintf("cleanup-%s", mig.Name()))
	if err := m.acquireLock(ctx, fmt.Sprintf("cleanup-%s", mig.Name())); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	m.logger.LogLockAcquired()
	defer m.releaseLock(ctx)

	m.logger.LogCleanupStart(mig.Name())

	// Run cleanup
	// Note: We're passing a minimal state object since the full state isn't stored yet
	cleanupState := &migration.State{
		MigrationName: mig.Name(),
		Phase:         "applied",
	}

	// Run cleanup (migration should not handle confirmations)
	err = mig.Cleanup(ctx, cleanupState)
	if err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	m.logger.LogCleanupComplete(legacyKeyCount)

	return nil
}

// Apply executes the migration
func (m *Migrator) Apply(ctx context.Context, autoApprove bool) error {
	// Get the next unapplied migration
	mig, err := m.getNextMigration(ctx)
	if err != nil {
		if err.Error() == "all migrations have been applied" {
			m.logger.LogAllMigrationsApplied()
			return nil
		}
		return err
	}

	// First show the plan
	m.logger.LogInfo("planning migration")
	plan, err := mig.Plan(ctx)
	if err != nil {
		return err
	}

	m.logger.LogMigrationPlan(mig.Name(), plan)

	// Confirm if not auto-approved
	if !autoApprove {
		confirmed, err := m.logger.Confirm("Do you want to apply these changes?")
		if err != nil {
			return err
		}
		if !confirmed {
			m.logger.LogMigrationCancelled()
			return nil
		}
	}

	// Acquire lock before applying migration
	m.logger.LogLockAcquiring("migration")
	if err := m.acquireLock(ctx, mig.Name()); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	m.logger.LogLockAcquired()
	defer m.releaseLock(ctx)

	// Apply the migration
	m.logger.LogMigrationStart(mig.Name())
	state, err := mig.Apply(ctx, plan)
	if err != nil {
		return err
	}

	// Mark migration as applied
	if err := setMigrationAsApplied(ctx, m.client, mig.Name()); err != nil {
		return fmt.Errorf("failed to mark migration as applied: %w", err)
	}

	stats := MigrationStats{
		ProcessedItems: state.Progress.ProcessedItems,
		FailedItems:    state.Progress.FailedItems,
		SkippedItems:   state.Progress.SkippedItems,
		Duration:       "", // TODO: Add duration tracking
	}
	m.logger.LogMigrationComplete(mig.Name(), stats)
	return nil
}

// isApplied checks if a migration has been applied
func isApplied(ctx context.Context, client *redisClientWrapper, name string) bool {
	key := fmt.Sprintf("outpost:migration:%s", name)
	val, err := client.HGet(ctx, key, "status").Result()
	if err != nil {
		return false
	}
	return val == "applied"
}

// getNextMigration finds the next unapplied migration
func (m *Migrator) getNextMigration(ctx context.Context) (migration.Migration, error) {
	// Sort migrations by version
	type migrationEntry struct {
		name      string
		migration migration.Migration
	}
	var sorted []migrationEntry
	for name, mig := range m.migrations {
		sorted = append(sorted, migrationEntry{name: name, migration: mig})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].migration.Version() < sorted[j].migration.Version()
	})

	// Find first unapplied
	for _, entry := range sorted {
		if !isApplied(ctx, m.client, entry.name) {
			return entry.migration, nil
		}
	}

	return nil, fmt.Errorf("all migrations have been applied")
}

// getLastAppliedMigration finds the most recently applied migration
func (m *Migrator) getLastAppliedMigration(ctx context.Context) (migration.Migration, error) {
	// Sort migrations by version (descending)
	type migrationEntry struct {
		name      string
		migration migration.Migration
	}
	var sorted []migrationEntry
	for name, mig := range m.migrations {
		sorted = append(sorted, migrationEntry{name: name, migration: mig})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].migration.Version() > sorted[j].migration.Version()
	})

	// Find last applied
	for _, entry := range sorted {
		if isApplied(ctx, m.client, entry.name) {
			return entry.migration, nil
		}
	}

	return nil, fmt.Errorf("no migrations have been applied")
}

// setMigrationAsApplied marks a migration as applied
func setMigrationAsApplied(ctx context.Context, client *redisClientWrapper, name string) error {
	key := fmt.Sprintf("outpost:migration:%s", name)

	// Use Redis hash to store migration state
	return client.HSet(ctx, key,
		"status", "applied",
		"applied_at", time.Now().Format(time.RFC3339),
	).Err()
}

// redisClientWrapper wraps go-redis Cmdable to implement the redis.Client interface
type redisClientWrapper struct {
	redis.Cmdable
}

func (r *redisClientWrapper) Close() error {
	// go-redis Cmdable doesn't have a Close method
	// This is a no-op for compatibility
	return nil
}

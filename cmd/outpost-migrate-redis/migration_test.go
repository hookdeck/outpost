package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	r "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMigration implements migratorredis.Migration for testing
type mockMigration struct {
	name         string
	version      int
	description  string
	autoRunnable bool

	planCalled   bool
	applyCalled  bool
	verifyCalled bool

	planFunc   func(ctx context.Context) (*migratorredis.Plan, error)
	applyFunc  func(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error)
	verifyFunc func(ctx context.Context, state *migratorredis.State) (*migratorredis.VerificationResult, error)
}

func newMockMigration(name string, version int) *mockMigration {
	return &mockMigration{
		name:         name,
		version:      version,
		description:  "Mock: " + name,
		autoRunnable: true,
	}
}

func (m *mockMigration) Name() string        { return m.name }
func (m *mockMigration) Version() int        { return m.version }
func (m *mockMigration) Description() string { return m.description }
func (m *mockMigration) AutoRunnable() bool  { return m.autoRunnable }

func (m *mockMigration) Plan(ctx context.Context) (*migratorredis.Plan, error) {
	m.planCalled = true
	if m.planFunc != nil {
		return m.planFunc(ctx)
	}
	return &migratorredis.Plan{
		MigrationName:  m.name,
		Description:    m.description,
		EstimatedItems: 10,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockMigration) Apply(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error) {
	m.applyCalled = true
	if m.applyFunc != nil {
		return m.applyFunc(ctx, plan)
	}
	now := time.Now()
	return &migratorredis.State{
		MigrationName: m.name,
		Phase:         "applied",
		CompletedAt:   &now,
		Progress: migratorredis.Progress{
			TotalItems:     10,
			ProcessedItems: 10,
		},
	}, nil
}

func (m *mockMigration) Verify(ctx context.Context, state *migratorredis.State) (*migratorredis.VerificationResult, error) {
	m.verifyCalled = true
	if m.verifyFunc != nil {
		return m.verifyFunc(ctx, state)
	}
	return &migratorredis.VerificationResult{Valid: true, ChecksRun: 1, ChecksPassed: 1}, nil
}

func (m *mockMigration) PlanCleanup(ctx context.Context) (int, error) {
	return 0, nil
}

func (m *mockMigration) Cleanup(ctx context.Context, state *migratorredis.State) error {
	return nil
}

// mockLogger implements MigrationLogger for testing
type mockLogger struct {
	confirmResult bool
	confirmError  error
}

func newMockLogger() *mockLogger {
	return &mockLogger{confirmResult: true}
}

func (l *mockLogger) Verbose() bool                                                   { return false }
func (l *mockLogger) LogRedisConfig(string, int, int, bool, bool, bool)               {}
func (l *mockLogger) LogInitialization(bool, int)                                     {}
func (l *mockLogger) LogMigrationList(map[string]string)                              {}
func (l *mockLogger) LogMigrationStatus(int, int)                                     {}
func (l *mockLogger) LogMigrationStart(string)                                        {}
func (l *mockLogger) LogMigrationPlan(string, *migratorredis.Plan, bool)              {}
func (l *mockLogger) LogMigrationProgress(string, int, int, string)                   {}
func (l *mockLogger) LogMigrationComplete(string, MigrationStats)                     {}
func (l *mockLogger) LogMigrationCancelled()                                          {}
func (l *mockLogger) LogVerificationStart(string)                                     {}
func (l *mockLogger) LogVerificationResult(string, *migratorredis.VerificationResult) {}
func (l *mockLogger) LogCleanupStart(string)                                          {}
func (l *mockLogger) LogCleanupAnalysis(int)                                          {}
func (l *mockLogger) LogCleanupComplete(int)                                          {}
func (l *mockLogger) LogNoCleanupNeeded()                                             {}
func (l *mockLogger) LogLockAcquiring(string)                                         {}
func (l *mockLogger) LogLockAcquired()                                                {}
func (l *mockLogger) LogLockReleased()                                                {}
func (l *mockLogger) LogLockWaiting()                                                 {}
func (l *mockLogger) LogLockStatus(string, bool)                                      {}
func (l *mockLogger) LogLockCleared()                                                 {}
func (l *mockLogger) LogCheckingInstallation()                                        {}
func (l *mockLogger) LogFreshInstallation()                                           {}
func (l *mockLogger) LogExistingInstallation()                                        {}
func (l *mockLogger) LogPendingMigrations(int)                                        {}
func (l *mockLogger) LogAllMigrationsApplied()                                        {}
func (l *mockLogger) LogNoMigrationsNeeded()                                          {}
func (l *mockLogger) LogInfo(string)                                                  {}
func (l *mockLogger) LogWarning(string)                                               {}
func (l *mockLogger) LogError(string, error)                                          {}
func (l *mockLogger) LogDebug(string)                                                 {}
func (l *mockLogger) LogProgress(int, int, string)                                    {}

func (l *mockLogger) Confirm(string) (bool, error) {
	return l.confirmResult, l.confirmError
}

func (l *mockLogger) ConfirmWithWarning(string, string) (bool, error) {
	return l.confirmResult, l.confirmError
}

func (l *mockLogger) Prompt(string) (string, error) {
	return "", nil
}

func setupTestMigrator(t *testing.T) (*Migrator, *miniredis.Miniredis, func()) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := r.NewClient(&r.Options{Addr: mr.Addr()})
	wrapper := &redisClientWrapper{Cmdable: client}

	migrator := &Migrator{
		client:     wrapper,
		logger:     newMockLogger(),
		migrations: make(map[string]migratorredis.Migration),
	}

	cleanup := func() {
		client.Close()
		mr.Close()
	}

	return migrator, mr, cleanup
}

// simulateExistingData adds data to Redis to simulate an existing installation
func simulateExistingData(mr *miniredis.Miniredis) {
	mr.HSet("outpost:tenant:test123:tenant", "id", "test123")
}

func TestMigrator_ApplyOne_NextPending(t *testing.T) {
	t.Run("applies next pending migration", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig

		err := migrator.ApplyOne(ctx, true, false, "")
		require.NoError(t, err)

		assert.True(t, mig.planCalled)
		assert.True(t, mig.applyCalled)
	})

	t.Run("returns nil when all applied", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig

		// Mark as applied
		mr.HSet("outpost:migration:001_test", "status", "applied")

		err := migrator.ApplyOne(ctx, true, false, "")
		require.NoError(t, err)

		assert.False(t, mig.applyCalled, "should not apply already-applied migration")
	})

	t.Run("applies in version order", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig1 := newMockMigration("001_first", 1)
		mig2 := newMockMigration("002_second", 2)
		migrator.migrations["001_first"] = mig1
		migrator.migrations["002_second"] = mig2

		// First apply
		err := migrator.ApplyOne(ctx, true, false, "")
		require.NoError(t, err)

		assert.True(t, mig1.applyCalled, "001 should be applied first")
		assert.False(t, mig2.applyCalled, "002 should not be applied yet")
	})
}

func TestMigrator_ApplyOne_SpecificMigration(t *testing.T) {
	t.Run("applies specific migration by name", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig1 := newMockMigration("001_first", 1)
		mig2 := newMockMigration("002_second", 2)
		migrator.migrations["001_first"] = mig1
		migrator.migrations["002_second"] = mig2

		// Apply specific migration (out of order)
		err := migrator.ApplyOne(ctx, true, false, "002_second")
		require.NoError(t, err)

		assert.False(t, mig1.applyCalled)
		assert.True(t, mig2.applyCalled)
	})

	t.Run("returns error for unknown migration", func(t *testing.T) {
		migrator, _, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		err := migrator.ApplyOne(ctx, true, false, "unknown_migration")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("skips already applied unless rerun", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig

		// Mark as applied
		mr.HSet("outpost:migration:001_test", "status", "applied")

		// Without rerun - should skip
		err := migrator.ApplyOne(ctx, true, false, "001_test")
		require.NoError(t, err)
		assert.False(t, mig.applyCalled)

		// With rerun - should apply
		err = migrator.ApplyOne(ctx, true, true, "001_test")
		require.NoError(t, err)
		assert.True(t, mig.applyCalled)
	})
}

func TestMigrator_ApplyOne_Confirmation(t *testing.T) {
	t.Run("skips confirmation with autoApprove", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		logger := newMockLogger()
		logger.confirmResult = false // Would reject if called
		migrator.logger = logger

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig

		err := migrator.ApplyOne(ctx, true, false, "") // autoApprove = true
		require.NoError(t, err)
		assert.True(t, mig.applyCalled)
	})

	t.Run("cancels when confirmation rejected", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		logger := newMockLogger()
		logger.confirmResult = false
		migrator.logger = logger

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig

		err := migrator.ApplyOne(ctx, false, false, "") // autoApprove = false
		require.NoError(t, err)
		assert.False(t, mig.applyCalled, "should not apply when confirmation rejected")
	})
}

func TestMigrator_ApplyOne_Failure(t *testing.T) {
	t.Run("returns error on plan failure", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_test", 1)
		mig.planFunc = func(ctx context.Context) (*migratorredis.Plan, error) {
			return nil, errors.New("plan failed")
		}
		migrator.migrations["001_test"] = mig

		err := migrator.ApplyOne(ctx, true, false, "")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "plan failed")
	})

	t.Run("returns error on apply failure", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_test", 1)
		mig.applyFunc = func(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error) {
			return nil, errors.New("apply failed")
		}
		migrator.migrations["001_test"] = mig

		err := migrator.ApplyOne(ctx, true, false, "")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "apply failed")
	})
}

func TestMigrator_ApplyOne_MarksAsApplied(t *testing.T) {
	t.Run("marks migration as applied after success", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig

		err := migrator.ApplyOne(ctx, true, false, "")
		require.NoError(t, err)

		// Verify it's marked in Redis
		status := mr.HGet("outpost:migration:001_test", "status")
		assert.Equal(t, "applied", status)
	})
}

func TestMigrator_Plan(t *testing.T) {
	t.Run("shows plan for next pending migration", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig

		err := migrator.Plan(ctx)
		require.NoError(t, err)

		assert.True(t, mig.planCalled)
		assert.False(t, mig.applyCalled, "Plan should not apply")
	})

	t.Run("returns nil when all applied", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig
		mr.HSet("outpost:migration:001_test", "status", "applied")

		err := migrator.Plan(ctx)
		require.NoError(t, err)

		assert.False(t, mig.planCalled)
	})
}

func TestMigrator_Verify(t *testing.T) {
	t.Run("verifies last applied migration", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig
		mr.HSet("outpost:migration:001_test", "status", "applied")

		err := migrator.Verify(ctx)
		require.NoError(t, err)

		assert.True(t, mig.verifyCalled)
	})

	t.Run("returns error when no migrations applied", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig
		// Not marking as applied

		err := migrator.Verify(ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no migrations have been applied")
	})
}

func TestMigrator_Apply_All(t *testing.T) {
	t.Run("applies all pending migrations in order", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		var executionOrder []string

		mig1 := newMockMigration("001_first", 1)
		mig1.applyFunc = func(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error) {
			executionOrder = append(executionOrder, "001_first")
			now := time.Now()
			return &migratorredis.State{
				MigrationName: "001_first",
				Phase:         "applied",
				CompletedAt:   &now,
				Progress:      migratorredis.Progress{TotalItems: 10, ProcessedItems: 10},
			}, nil
		}

		mig2 := newMockMigration("002_second", 2)
		mig2.applyFunc = func(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error) {
			executionOrder = append(executionOrder, "002_second")
			now := time.Now()
			return &migratorredis.State{
				MigrationName: "002_second",
				Phase:         "applied",
				CompletedAt:   &now,
				Progress:      migratorredis.Progress{TotalItems: 5, ProcessedItems: 5},
			}, nil
		}

		mig3 := newMockMigration("003_third", 3)
		mig3.applyFunc = func(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error) {
			executionOrder = append(executionOrder, "003_third")
			now := time.Now()
			return &migratorredis.State{
				MigrationName: "003_third",
				Phase:         "applied",
				CompletedAt:   &now,
				Progress:      migratorredis.Progress{TotalItems: 3, ProcessedItems: 3},
			}, nil
		}

		// Register in wrong order to test sorting
		migrator.migrations["003_third"] = mig3
		migrator.migrations["001_first"] = mig1
		migrator.migrations["002_second"] = mig2

		err := migrator.Apply(ctx, true)
		require.NoError(t, err)

		assert.Equal(t, []string{"001_first", "002_second", "003_third"}, executionOrder)
	})

	t.Run("returns nil when all already applied", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig

		// Mark as applied
		mr.HSet("outpost:migration:001_test", "status", "applied")

		err := migrator.Apply(ctx, true)
		require.NoError(t, err)

		assert.False(t, mig.applyCalled, "should not apply already-applied migration")
	})

	t.Run("stops on migration failure", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig1 := newMockMigration("001_first", 1)
		mig1.applyFunc = func(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error) {
			return nil, errors.New("migration failed")
		}

		mig2 := newMockMigration("002_second", 2)

		migrator.migrations["001_first"] = mig1
		migrator.migrations["002_second"] = mig2

		err := migrator.Apply(ctx, true)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "001_first")
		assert.False(t, mig2.applyCalled, "second migration should not run after failure")
	})

	t.Run("stops on verification failure", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig1 := newMockMigration("001_first", 1)
		mig1.verifyFunc = func(ctx context.Context, state *migratorredis.State) (*migratorredis.VerificationResult, error) {
			return &migratorredis.VerificationResult{
				Valid:  false,
				Issues: []string{"something went wrong"},
			}, nil
		}

		mig2 := newMockMigration("002_second", 2)

		migrator.migrations["001_first"] = mig1
		migrator.migrations["002_second"] = mig2

		err := migrator.Apply(ctx, true)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "verification failed")
		assert.True(t, mig1.applyCalled, "first migration should be applied")
		assert.False(t, mig2.applyCalled, "second migration should not run after verification failure")
	})

	t.Run("cancels when confirmation rejected", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		logger := newMockLogger()
		logger.confirmResult = false
		migrator.logger = logger

		mig := newMockMigration("001_test", 1)
		migrator.migrations["001_test"] = mig

		err := migrator.Apply(ctx, false) // autoApprove = false
		require.NoError(t, err)
		assert.False(t, mig.applyCalled, "should not apply when confirmation rejected")
	})

	t.Run("marks all migrations as applied", func(t *testing.T) {
		migrator, mr, cleanup := setupTestMigrator(t)
		defer cleanup()
		ctx := context.Background()

		simulateExistingData(mr)

		mig1 := newMockMigration("001_first", 1)
		mig2 := newMockMigration("002_second", 2)

		migrator.migrations["001_first"] = mig1
		migrator.migrations["002_second"] = mig2

		err := migrator.Apply(ctx, true)
		require.NoError(t, err)

		// Verify both are marked in Redis
		status1 := mr.HGet("outpost:migration:001_first", "status")
		status2 := mr.HGet("outpost:migration:002_second", "status")
		assert.Equal(t, "applied", status1)
		assert.Equal(t, "applied", status2)
	})
}

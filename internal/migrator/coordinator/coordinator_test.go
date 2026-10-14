package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	"github.com/hookdeck/outpost/internal/redis"
	r "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redisTestClient wraps miniredis client to implement redis.Client.
type redisTestClient struct {
	*r.Client
}

func (c *redisTestClient) Close() error              { return c.Client.Close() }
func (c *redisTestClient) Pipeline() redis.Pipeliner { return c.Client.Pipeline() }

// fakeMigration is a test-only migratorredis.Migration that records calls.
type fakeMigration struct {
	name           string
	version        int
	description    string
	applicable     bool
	notAppReason   string
	estimatedItems int

	planCalled   bool
	applyCalled  bool
	verifyCalled bool
	verifyValid  bool

	cleanupItems  int
	cleanupCalled bool
}

func newFakeMigration(name string, version int) *fakeMigration {
	return &fakeMigration{
		name:           name,
		version:        version,
		description:    "fake migration " + name,
		applicable:     true,
		estimatedItems: 5,
		verifyValid:    true,
	}
}

func (m *fakeMigration) Name() string        { return m.name }
func (m *fakeMigration) Version() int        { return m.version }
func (m *fakeMigration) Description() string { return m.description }
func (m *fakeMigration) AutoRunnable() bool  { return false }

func (m *fakeMigration) IsApplicable(ctx context.Context) (bool, string) {
	return m.applicable, m.notAppReason
}

func (m *fakeMigration) Plan(ctx context.Context) (*migratorredis.Plan, error) {
	m.planCalled = true
	return &migratorredis.Plan{
		MigrationName:  m.name,
		Description:    m.description,
		EstimatedItems: m.estimatedItems,
		Scope:          map[string]int{"items": m.estimatedItems},
		Timestamp:      time.Now(),
	}, nil
}

func (m *fakeMigration) Apply(ctx context.Context, plan *migratorredis.Plan) (*migratorredis.State, error) {
	m.applyCalled = true
	now := time.Now()
	return &migratorredis.State{
		MigrationName: m.name,
		Phase:         "applied",
		StartedAt:     now,
		CompletedAt:   &now,
		Progress: migratorredis.Progress{
			TotalItems:     plan.EstimatedItems,
			ProcessedItems: plan.EstimatedItems,
		},
	}, nil
}

func (m *fakeMigration) Verify(ctx context.Context, state *migratorredis.State) (*migratorredis.VerificationResult, error) {
	m.verifyCalled = true
	return &migratorredis.VerificationResult{
		Valid:        m.verifyValid,
		ChecksRun:    1,
		ChecksPassed: 1,
	}, nil
}

func (m *fakeMigration) PlanCleanup(ctx context.Context) (int, error) { return m.cleanupItems, nil }
func (m *fakeMigration) Cleanup(ctx context.Context, state *migratorredis.State) error {
	m.cleanupCalled = true
	m.cleanupItems = 0
	return nil
}

func newTestCoordinator(t *testing.T, migrations ...migratorredis.Migration) (*Coordinator, *miniredis.Miniredis, func()) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := r.NewClient(&r.Options{Addr: mr.Addr()})
	testClient := &redisTestClient{Client: client}

	logger, err := logging.NewLogger(logging.WithLogLevel("error"))
	require.NoError(t, err)

	c := New(Config{
		RedisClient:     testClient,
		RedisMigrations: migrations,
		Logger:          logger,
	})
	return c, mr, func() {
		client.Close()
		mr.Close()
	}
}

func TestCoordinator_List_RedisOnly(t *testing.T) {
	m1 := newFakeMigration("001_first", 1)
	m2 := newFakeMigration("002_second", 2)
	c, _, cleanup := newTestCoordinator(t, m1, m2)
	defer cleanup()

	ctx := context.Background()

	// Nothing applied yet — both should be pending.
	list, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "redis/001_first", list[0].ID)
	assert.Equal(t, StatusPending, list[0].Status)
	assert.Equal(t, StatusPending, list[1].Status)
}

// TestCoordinator_List_ReportsNotApplicable verifies that list agrees with
// PendingSummary: an unstarted migration whose IsApplicable returns false
// must be surfaced as not_applicable rather than pending, otherwise list
// and the startup gate disagree on the Redis pending count.
func TestCoordinator_List_ReportsNotApplicable(t *testing.T) {
	m1 := newFakeMigration("001_first", 1)
	m1.applicable = false
	m1.notAppReason = "not needed for this config"
	m2 := newFakeMigration("002_second", 2)
	c, _, cleanup := newTestCoordinator(t, m1, m2)
	defer cleanup()

	ctx := context.Background()

	list, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)

	assert.Equal(t, "redis/001_first", list[0].ID)
	assert.Equal(t, StatusNotApplicable, list[0].Status,
		"unstarted migration with IsApplicable=false should surface as not_applicable")
	assert.Equal(t, "not needed for this config", list[0].Reason)

	assert.Equal(t, "redis/002_second", list[1].ID)
	assert.Equal(t, StatusPending, list[1].Status)

	// And summary must agree.
	summary, err := c.PendingSummary(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.RedisPending,
		"summary must agree with list: exactly one migration is effectively pending")
}

func TestCoordinator_PendingSummary(t *testing.T) {
	m1 := newFakeMigration("001_first", 1)
	m2 := newFakeMigration("002_second", 2)
	c, _, cleanup := newTestCoordinator(t, m1, m2)
	defer cleanup()

	ctx := context.Background()

	summary, err := c.PendingSummary(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.RedisPending)
	assert.True(t, summary.HasPending())
}

func TestCoordinator_PendingSummary_SkipsNotApplicable(t *testing.T) {
	m1 := newFakeMigration("001_first", 1)
	m1.applicable = false
	m1.notAppReason = "not needed for this config"
	m2 := newFakeMigration("002_second", 2)
	c, _, cleanup := newTestCoordinator(t, m1, m2)
	defer cleanup()

	ctx := context.Background()

	summary, err := c.PendingSummary(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.RedisPending,
		"non-applicable migrations should not count as pending")
}

func TestCoordinator_Apply_RunsRedisMigrations(t *testing.T) {
	m1 := newFakeMigration("001_first", 1)
	m2 := newFakeMigration("002_second", 2)
	c, _, cleanup := newTestCoordinator(t, m1, m2)
	defer cleanup()

	ctx := context.Background()

	require.NoError(t, c.Apply(ctx, ApplyOptions{}))

	assert.True(t, m1.planCalled)
	assert.True(t, m1.applyCalled)
	assert.True(t, m2.planCalled)
	assert.True(t, m2.applyCalled)

	// Subsequent List should report both as applied.
	list, err := c.List(ctx)
	require.NoError(t, err)
	for _, info := range list {
		assert.Equal(t, StatusApplied, info.Status, "migration %s", info.ID)
	}

	// Summary should now be empty.
	summary, err := c.PendingSummary(ctx)
	require.NoError(t, err)
	assert.False(t, summary.HasPending())
}

func TestCoordinator_Apply_MarksNotApplicable(t *testing.T) {
	m1 := newFakeMigration("001_first", 1)
	m1.applicable = false
	m1.notAppReason = "deployment_id set"
	m2 := newFakeMigration("002_second", 2)
	c, _, cleanup := newTestCoordinator(t, m1, m2)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, c.Apply(ctx, ApplyOptions{}))

	// m1 should not have been applied, just marked.
	assert.False(t, m1.applyCalled)
	assert.True(t, m2.applyCalled)

	list, err := c.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, StatusNotApplicable, list[0].Status)
	assert.Equal(t, "deployment_id set", list[0].Reason)
	assert.Equal(t, StatusApplied, list[1].Status)
}

func TestCoordinator_Apply_Idempotent(t *testing.T) {
	m1 := newFakeMigration("001_first", 1)
	c, _, cleanup := newTestCoordinator(t, m1)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, c.Apply(ctx, ApplyOptions{}))

	// Reset call tracking and run again — should be a no-op.
	m1.planCalled = false
	m1.applyCalled = false

	require.NoError(t, c.Apply(ctx, ApplyOptions{}))
	assert.False(t, m1.planCalled, "already-applied migration should not be re-planned")
	assert.False(t, m1.applyCalled, "already-applied migration should not be re-applied")
}

func TestCoordinator_Plan(t *testing.T) {
	m1 := newFakeMigration("001_first", 1)
	m1.estimatedItems = 42
	c, _, cleanup := newTestCoordinator(t, m1)
	defer cleanup()

	ctx := context.Background()
	plan, err := c.Plan(ctx)
	require.NoError(t, err)

	require.Len(t, plan.Redis, 1)
	assert.Equal(t, "001_first", plan.Redis[0].Name)
	assert.Equal(t, 42, plan.Redis[0].EstimatedItems)
	assert.True(t, plan.HasChanges())
}

func TestCoordinator_Verify(t *testing.T) {
	m1 := newFakeMigration("001_first", 1)
	c, _, cleanup := newTestCoordinator(t, m1)
	defer cleanup()

	ctx := context.Background()
	require.NoError(t, c.Apply(ctx, ApplyOptions{}))

	report, err := c.Verify(ctx)
	require.NoError(t, err)
	require.Len(t, report.RedisResults, 1)
	assert.True(t, report.RedisResults[0].Valid)
	assert.True(t, m1.verifyCalled)
	assert.True(t, report.Ok())
}

func TestCoordinator_Unlock(t *testing.T) {
	c, mr, cleanup := newTestCoordinator(t)
	defer cleanup()

	ctx := context.Background()

	// Plant a stale lock.
	mr.Set(".outpost:migration_lock", "stale")

	require.NoError(t, c.Unlock(ctx))

	_, err := mr.Get(".outpost:migration_lock")
	assert.Error(t, err, "lock should have been cleared")
}

func TestCoordinator_DeploymentID_Scoping(t *testing.T) {
	m1 := newFakeMigration("001_first", 1)

	mr := miniredis.RunT(t)
	client := r.NewClient(&r.Options{Addr: mr.Addr()})
	defer client.Close()

	logger, err := logging.NewLogger(logging.WithLogLevel("error"))
	require.NoError(t, err)

	c := New(Config{
		RedisClient:     &redisTestClient{Client: client},
		RedisMigrations: []migratorredis.Migration{m1},
		DeploymentID:    "dp_test",
		Logger:          logger,
	})

	ctx := context.Background()
	require.NoError(t, c.Apply(ctx, ApplyOptions{}))

	// Verify the hash key was written with the deployment prefix.
	val, err := mr.Get("dp_test:outpost:migration:001_first")
	if err == nil {
		t.Log("got:", val)
	}
	// HGET via miniredis: use HGet method
	status := mr.HGet("dp_test:outpost:migration:001_first", "status")
	assert.Equal(t, "applied", status,
		"migration key should be scoped to deployment_id prefix")
}

func TestCoordinator_Cleanup(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*Coordinator, *fakeMigration, *fakeMigration, *fakeMigration) {
		applied := newFakeMigration("001_applied", 1)
		applied.cleanupItems = 3
		notApplicable := newFakeMigration("002_not_applicable", 2)
		notApplicable.applicable = false
		notApplicable.notAppReason = "not needed"
		notApplicable.cleanupItems = 5
		other := newFakeMigration("003_other", 3)
		other.cleanupItems = 2
		c, _, cleanup := newTestCoordinator(t, applied, notApplicable, other)
		t.Cleanup(cleanup)
		require.NoError(t, c.Apply(ctx, ApplyOptions{}))
		return c, applied, notApplicable, other
	}

	t.Run("plan lists applied migrations with data left", func(t *testing.T) {
		c, _, _, other := setup(t)
		other.cleanupItems = 0
		plans, err := c.PlanCleanup(ctx)
		require.NoError(t, err)
		require.Len(t, plans, 1)
		assert.Equal(t, "001_applied", plans[0].Name)
		assert.Equal(t, 3, plans[0].Items)
	})

	t.Run("runs only the named migration", func(t *testing.T) {
		c, applied, notApplicable, other := setup(t)
		plan, err := c.PlanCleanupFor(ctx, "redis/001_applied")
		require.NoError(t, err)
		assert.Equal(t, 3, plan.Items)

		require.NoError(t, c.Cleanup(ctx, "001_applied"))
		assert.True(t, applied.cleanupCalled)
		assert.False(t, notApplicable.cleanupCalled)
		assert.False(t, other.cleanupCalled)
	})

	t.Run("not applicable has nothing to clean up", func(t *testing.T) {
		c, _, _, _ := setup(t)
		plan, err := c.PlanCleanupFor(ctx, "002_not_applicable")
		require.NoError(t, err)
		assert.Equal(t, "not needed", plan.NotApplicableReason)
		assert.Equal(t, 0, plan.Items)
		assert.ErrorIs(t, c.Cleanup(ctx, "002_not_applicable"), ErrNotApplied)
	})

	t.Run("pending and unknown migrations", func(t *testing.T) {
		pending := newFakeMigration("001_pending", 1)
		c, _, cleanup := newTestCoordinator(t, pending)
		defer cleanup()
		_, err := c.PlanCleanupFor(ctx, "001_pending")
		assert.ErrorIs(t, err, ErrNotApplied)
		assert.ErrorIs(t, c.Cleanup(ctx, "001_pending"), ErrNotApplied)
		assert.False(t, pending.cleanupCalled)
		_, err = c.PlanCleanupFor(ctx, "999_nope")
		assert.ErrorIs(t, err, ErrUnknownMigration)
	})

	t.Run("fails while the migration lock is held", func(t *testing.T) {
		c, applied, _, _ := setup(t)
		ok, err := c.newRedisLock().AttemptLock(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Error(t, c.Cleanup(ctx, "001_applied"))
		assert.False(t, applied.cleanupCalled)
	})
}

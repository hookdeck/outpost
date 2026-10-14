package deliverymq_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	migration_004 "github.com/hookdeck/outpost/internal/migrator/migratorredis/004_rsmq_hash_tags"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rsmq"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyRetryQueue schedules retries the way versions before hash-tagged
// rsmq keys did, and reports how many remain in that queue.
type legacyRetryQueue struct {
	scheduler scheduler.Scheduler
	client    redis.Client
	ns        string
}

func newLegacyRetryQueue(t *testing.T, ctx context.Context, cfg *redis.RedisConfig, ns string) *legacyRetryQueue {
	client, err := redis.New(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })
	s := scheduler.New("deliverymq-retry", rsmq.NewRedisSMQ(rsmq.NewRedisAdapter(client), ns, rsmq.WithUntaggedKeys()), nil)
	require.NoError(t, s.Init(ctx))
	return &legacyRetryQueue{scheduler: s, client: client, ns: ns}
}

func (q *legacyRetryQueue) schedule(t *testing.T, ctx context.Context, task deliverymq.RetryTask, delay time.Duration) {
	msg, err := task.ToString()
	require.NoError(t, err)
	retryID := models.RetryID(task.EventID, task.DestinationID)
	require.NoError(t, q.scheduler.Schedule(ctx, msg, delay, scheduler.WithTaskID(retryID)))
}

func (q *legacyRetryQueue) len(t *testing.T, ctx context.Context) int64 {
	n, err := q.client.ZCard(ctx, q.ns+":deliverymq-retry").Result()
	require.NoError(t, err)
	return n
}

func TestRetryScheduler_MigratedLegacyQueue(t *testing.T) {
	tests := []struct {
		name         string
		deploymentID string
		legacyNs     string
		currentKey   string
	}{
		{name: "without deployment id", deploymentID: "", legacyNs: "rsmq", currentKey: "rsmq:{deliverymq-retry}"},
		{name: "with deployment id", deploymentID: "dp_legacy", legacyNs: "dp_legacy:rsmq", currentKey: "dp_legacy:rsmq:{dp_legacy:deliverymq-retry}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testRetrySchedulerMigratedLegacyQueue(t, tt.deploymentID, tt.legacyNs, tt.currentKey)
		})
	}
}

func testRetrySchedulerMigratedLegacyQueue(t *testing.T, deploymentID, legacyNs, currentKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenant := models.Tenant{ID: idgen.String()}
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithTenantID(tenant.ID),
	)
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTenantID(tenant.ID),
		testutil.EventFactory.WithDestinationID(destination.ID),
		testutil.EventFactory.WithEligibleForRetry(true),
	)
	eventGetter := newMockEventGetter()
	eventGetter.addRecord(&models.Attempt{
		ID:            idgen.Attempt(),
		EventID:       event.ID,
		DestinationID: destination.ID,
		AttemptNumber: 1,
	}, &event)

	redisConfig := testutil.CreateTestRedisConfig(t)
	legacy := newLegacyRetryQueue(t, ctx, redisConfig, legacyNs)
	retryTask := deliverymq.RetryTask{EventID: event.ID, TenantID: tenant.ID, DestinationID: destination.ID}

	// A retry scheduled by the previous version, then the migration.
	legacy.schedule(t, ctx, retryTask, 0)
	require.Equal(t, int64(1), legacy.len(t, ctx))
	m := migration_004.New(legacy.client, migratorredis.NewLoggerAdapter(testutil.CreateTestLogger(t), false), deploymentID)
	plan, err := m.Plan(ctx)
	require.NoError(t, err)
	_, err = m.Apply(ctx, plan)
	require.NoError(t, err)
	require.Equal(t, int64(0), legacy.len(t, ctx))

	mqConfig := &mqs.QueueConfig{InMemory: &mqs.InMemoryConfig{Name: testutil.RandomString(5)}}
	deliveryMQ := deliverymq.New(deliverymq.WithQueue(mqConfig))
	cleanup, err := deliveryMQ.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	retryScheduler, err := deliverymq.NewRetryScheduler(deliveryMQ, redisConfig, deploymentID, 10*time.Millisecond, testutil.CreateTestLogger(t), eventGetter)
	require.NoError(t, err)
	require.NoError(t, retryScheduler.Init(ctx))
	defer retryScheduler.Shutdown()

	subscription, err := deliveryMQ.Subscribe(ctx)
	require.NoError(t, err)
	defer subscription.Shutdown(ctx)
	go retryScheduler.Monitor(ctx)

	msg, err := subscription.Receive(ctx)
	require.NoError(t, err)
	msg.Ack()
	var task models.DeliveryTask
	require.NoError(t, task.FromMessage(msg))
	assert.Equal(t, event.ID, task.Event.ID)
	assert.Equal(t, destination.ID, task.DestinationID)
	assert.Equal(t, 2, task.Attempt)

	require.Eventually(t, func() bool {
		n, err := legacy.client.ZCard(ctx, currentKey).Result()
		return err == nil && n == 0
	}, 2*time.Second, 10*time.Millisecond, "retry should be deleted after it fires")

	t.Run("new retries go to the hash-tagged queue", func(t *testing.T) {
		msg, err := retryTask.ToString()
		require.NoError(t, err)
		retryID := models.RetryID(event.ID, destination.ID)
		require.NoError(t, retryScheduler.Schedule(ctx, msg, time.Hour, scheduler.WithTaskID(retryID)))
		n, err := legacy.client.ZCard(ctx, currentKey).Result()
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		assert.Equal(t, int64(0), legacy.len(t, ctx))
	})
}

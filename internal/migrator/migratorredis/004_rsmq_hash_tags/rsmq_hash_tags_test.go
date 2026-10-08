package migration_004_rsmq_hash_tags_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	migration "github.com/hookdeck/outpost/internal/migrator/migratorredis/004_rsmq_hash_tags"

	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rsmq"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const queue = "deliverymq-retry"

func TestRSMQHashTags_Miniredis(t *testing.T) {
	runSuite(t, func(t *testing.T) *redis.RedisConfig { return testutil.CreateTestRedisConfig(t) })
}

func TestRSMQHashTags_Dragonfly(t *testing.T) {
	testinfra.Start(t)
	runSuite(t, testinfra.NewDragonflyConfig)
}

func TestRSMQHashTags_Redis(t *testing.T) {
	testutil.SkipUnlessCompat(t)
	testinfra.Start(t)
	runSuite(t, testinfra.NewRedisConfig)
}

func TestRSMQHashTags_NotApplicableOnCluster(t *testing.T) {
	client := goredis.NewClusterClient(&goredis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}})
	defer client.Close()
	m := migration.New(client, newLogger(t), "")
	applicable, reason := m.IsApplicable(context.Background())
	assert.False(t, applicable)
	assert.Contains(t, reason, "Redis Cluster")
}

func runSuite(t *testing.T, newConfig func(t *testing.T) *redis.RedisConfig) {
	tests := []struct {
		name         string
		deploymentID string
		ns           string
		tagPrefix    string
		oldKey       string
		taggedKey    string
	}{
		{name: "without deployment id", ns: "rsmq", oldKey: "rsmq:deliverymq-retry", taggedKey: "rsmq:{deliverymq-retry}"},
		{name: "with deployment id", deploymentID: "dp_1", ns: "dp_1:rsmq", tagPrefix: "dp_1:", oldKey: "dp_1:rsmq:deliverymq-retry", taggedKey: "dp_1:rsmq:{dp_1:deliverymq-retry}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newConfig(t)
			ctx := context.Background()
			newEnv := func(t *testing.T) *env {
				client, err := redis.New(ctx, cfg)
				require.NoError(t, err)
				t.Cleanup(func() { client.Close() })
				require.NoError(t, client.FlushDB(ctx).Err())
				adapter := rsmq.NewRedisAdapter(client)
				e := &env{
					client:    client,
					old:       rsmq.NewRedisSMQ(adapter, tt.ns, rsmq.WithUntaggedKeys()),
					tagged:    rsmq.NewRedisSMQ(adapter, tt.ns, rsmq.WithHashTagPrefix(tt.tagPrefix)),
					migration: migration.New(client, newLogger(t), tt.deploymentID),
					oldKey:    tt.oldKey,
					taggedKey: tt.taggedKey,
				}
				require.NoError(t, e.old.CreateQueue(queue, 30, 0, -1))
				require.NoError(t, e.old.CreateQueue(queue+"-dlq", 30, 0, -1))
				return e
			}

			t.Run("apply moves messages to the tagged keys", func(t *testing.T) { testApplyMoves(t, newEnv(t)) })
			t.Run("old layout sees an empty queue after apply", func(t *testing.T) { testOldLayoutAfterApply(t, newEnv(t)) })
			t.Run("apply keeps existing tagged queue settings", func(t *testing.T) { testApplyKeepsTaggedSettings(t, newEnv(t)) })
			t.Run("cleanup moves leftovers, newest wins, deletes old keys", func(t *testing.T) { testCleanup(t, newEnv(t)) })
			t.Run("apply moves more than one batch", func(t *testing.T) { testApplyBatches(t, newEnv(t)) })
			t.Run("apply and cleanup on an empty store", func(t *testing.T) { testEmpty(t, newEnv(t)) })
		})
	}
}

type env struct {
	client    redis.Client
	old       *rsmq.RedisSMQ
	tagged    *rsmq.RedisSMQ
	migration *migration.RSMQHashTagsMigration
	oldKey    string
	taggedKey string
}

func (e *env) apply(t *testing.T) *migratorredis.State {
	ctx := context.Background()
	applicable, _ := e.migration.IsApplicable(ctx)
	require.True(t, applicable)
	plan, err := e.migration.Plan(ctx)
	require.NoError(t, err)
	state, err := e.migration.Apply(ctx, plan)
	require.NoError(t, err)
	return state
}

func testApplyMoves(t *testing.T, e *env) {
	ctx := context.Background()

	_, err := e.old.SendMessage(queue, "due", 0, rsmq.WithMessageID(id("due")))
	require.NoError(t, err)
	_, err = e.old.SendMessage(queue, "later", 3600, rsmq.WithMessageID(id("later")))
	require.NoError(t, err)
	_, err = e.old.SendMessage(queue, "inflight", 0, rsmq.WithMessageID(id("inflight")))
	require.NoError(t, err)
	_, err = e.old.SendMessage(queue+"-dlq", "dead", 0, rsmq.WithMessageID(id("dead")))
	require.NoError(t, err)

	// Receive "inflight" (and "due", which comes first) with a long visibility
	// timeout, as an instance processing it would; put "due" back.
	for i := 0; i < 2; i++ {
		msg, err := e.old.ReceiveMessage(queue, 3600)
		require.NoError(t, err)
		require.NotNil(t, msg)
	}
	require.NoError(t, e.old.ChangeMessageVisibility(queue, id("due"), 0))

	scores := map[string]float64{}
	for _, name := range []string{"due", "later", "inflight"} {
		s, err := e.client.ZScore(ctx, e.oldKey, id(name)).Result()
		require.NoError(t, err)
		scores[id(name)] = s
	}
	oldRC, err := e.client.HGet(ctx, e.oldKey+":Q", id("inflight")+":rc").Result()
	require.NoError(t, err)
	oldFR, err := e.client.HGet(ctx, e.oldKey+":Q", id("inflight")+":fr").Result()
	require.NoError(t, err)

	plan, err := e.migration.Plan(ctx)
	require.NoError(t, err)
	assert.Equal(t, 4, plan.EstimatedItems)
	assert.Equal(t, 3, plan.Scope[queue])
	assert.Equal(t, 1, plan.Scope[queue+"-dlq"])

	state, err := e.migration.Apply(ctx, plan)
	require.NoError(t, err)
	assert.Equal(t, 4, state.Progress.ProcessedItems)

	for msgID, score := range scores {
		s, err := e.client.ZScore(ctx, e.taggedKey, msgID).Result()
		require.NoError(t, err, msgID)
		assert.Equal(t, score, s, msgID)
	}
	rc, err := e.client.HGet(ctx, e.taggedKey+":Q", id("inflight")+":rc").Result()
	require.NoError(t, err)
	assert.Equal(t, oldRC, rc)
	fr, err := e.client.HGet(ctx, e.taggedKey+":Q", id("inflight")+":fr").Result()
	require.NoError(t, err)
	assert.Equal(t, oldFR, fr)

	attrs, err := e.tagged.GetQueueAttributes(queue)
	require.NoError(t, err)
	assert.Equal(t, uint(30), attrs.Vt)
	assert.Equal(t, uint64(3), attrs.Msgs)
	assert.Equal(t, uint64(2), attrs.HiddenMsgs)

	msg, err := e.tagged.ReceiveMessage(queue, 30)
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, id("due"), msg.ID)
	assert.Equal(t, "due", msg.Message)

	dead, err := e.tagged.ReceiveMessage(queue+"-dlq", 30)
	require.NoError(t, err)
	require.NotNil(t, dead)
	assert.Equal(t, "dead", dead.Message)

	n, err := e.client.ZCard(ctx, e.oldKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)

	result, err := e.migration.Verify(ctx, state)
	require.NoError(t, err)
	assert.True(t, result.Valid, result.Issues)
}

func testOldLayoutAfterApply(t *testing.T, e *env) {
	_, err := e.old.SendMessage(queue, "msg", 0, rsmq.WithMessageID(id("a")))
	require.NoError(t, err)
	e.apply(t)

	msg, err := e.old.ReceiveMessage(queue, 30)
	require.NoError(t, err)
	assert.Nil(t, msg)
	poll, err := e.old.ReceiveMessagePoll(queue, 30)
	require.NoError(t, err)
	assert.Nil(t, poll.Message)
	assert.False(t, poll.HasNext)
	// Acking a message that was moved finds nothing.
	assert.ErrorIs(t, e.old.DeleteMessage(queue, id("a")), rsmq.ErrMessageNotFound)

	// It can still schedule; cleanup picks those up later.
	_, err = e.old.SendMessage(queue, "msg", 0, rsmq.WithMessageID(id("b")))
	require.NoError(t, err)
	msg, err = e.old.ReceiveMessage(queue, 30)
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, id("b"), msg.ID)
}

func testApplyKeepsTaggedSettings(t *testing.T, e *env) {
	require.NoError(t, e.tagged.CreateQueue(queue, 99, 0, -1))
	e.apply(t)
	attrs, err := e.tagged.GetQueueAttributes(queue)
	require.NoError(t, err)
	assert.Equal(t, uint(99), attrs.Vt)
	// The dlq had no tagged queue: created with the old settings.
	attrs, err = e.tagged.GetQueueAttributes(queue + "-dlq")
	require.NoError(t, err)
	assert.Equal(t, uint(30), attrs.Vt)
}

func testCleanup(t *testing.T, e *env) {
	ctx := context.Background()
	state := e.apply(t)

	// During the rollout, an instance of the previous version schedules two
	// retries; a new instance schedules one of them again.
	_, err := e.old.SendMessage(queue, "old-a", 0, rsmq.WithMessageID(id("a")))
	require.NoError(t, err)
	_, err = e.old.SendMessage(queue, "old-b", 0, rsmq.WithMessageID(id("b")))
	require.NoError(t, err)
	_, err = e.tagged.SendMessage(queue, "new-a", 3600, rsmq.WithMessageID(id("a")))
	require.NoError(t, err)

	result, err := e.migration.Verify(ctx, state)
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Issues, 1)

	n, err := e.migration.PlanCleanup(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, n) // retry zset + hash, dlq hash (its zset is empty)

	require.NoError(t, e.migration.Cleanup(ctx, state))

	body, err := e.client.HGet(ctx, e.taggedKey+":Q", id("a")).Result()
	require.NoError(t, err)
	assert.Equal(t, "new-a", body)
	body, err = e.client.HGet(ctx, e.taggedKey+":Q", id("b")).Result()
	require.NoError(t, err)
	assert.Equal(t, "old-b", body)

	exists, err := e.client.Exists(ctx, e.oldKey, e.oldKey+":Q", e.oldKey+"-dlq", e.oldKey+"-dlq:Q").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), exists)

	n, err = e.migration.PlanCleanup(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	result, err = e.migration.Verify(ctx, state)
	require.NoError(t, err)
	assert.True(t, result.Valid, result.Issues)

	msg, err := e.tagged.ReceiveMessage(queue, 30)
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, id("b"), msg.ID)
}

func testApplyBatches(t *testing.T, e *env) {
	ctx := context.Background()
	const count = 2500
	for i := 0; i < count; i++ {
		_, err := e.old.SendMessage(queue, "msg", 0, rsmq.WithMessageID(id(fmt.Sprintf("m%05d", i))))
		require.NoError(t, err)
	}
	state := e.apply(t)
	assert.Equal(t, count, state.Progress.ProcessedItems)
	n, err := e.client.ZCard(ctx, e.taggedKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(count), n)
	n, err = e.client.ZCard(ctx, e.oldKey).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
}

func testEmpty(t *testing.T, e *env) {
	ctx := context.Background()
	require.NoError(t, e.client.FlushDB(ctx).Err())
	state := e.apply(t)
	assert.Equal(t, 0, state.Progress.ProcessedItems)
	require.NoError(t, e.migration.Cleanup(ctx, state))
	keys, err := e.client.Keys(ctx, "*").Result()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

// id pads name to a valid rsmq message ID.
func id(name string) string {
	return name + strings.Repeat("0", 32-len(name))
}

func newLogger(t *testing.T) migratorredis.Logger {
	return migratorredis.NewLoggerAdapter(testutil.CreateTestLogger(t), false)
}

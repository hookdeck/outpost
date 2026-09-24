package publishmq_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisRedeliveryCounter(t *testing.T) {
	ctx := context.Background()
	client := testutil.CreateTestRedisClient(t)
	counter := publishmq.NewRedisRedeliveryCounter(client, "dep_1")

	for want := int64(1); want <= 3; want++ {
		n, err := counter.Incr(ctx, "msg_1")
		require.NoError(t, err)
		assert.Equal(t, want, n)
	}

	n, err := counter.Incr(ctx, "msg_2")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "keys are counted separately")

	ttl, err := client.TTL(ctx, "dep_1:publishmq:failures:msg_1").Result()
	require.NoError(t, err)
	assert.Greater(t, ttl, 23*time.Hour, "count expires")
}

package deliverystatus_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/deliverystatus"
	"github.com/hookdeck/outpost/internal/models"
	internalredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The record script against real servers: Redis returns HGET values to Lua as
// strings, Dragonfly as numbers; both must compare numerically.
func TestIntegration_RecordAttempts(t *testing.T) {
	t.Parallel()
	testinfra.Start(t)

	for name, cfg := range map[string]func(*testing.T) *internalredis.RedisConfig{
		"redis":     testinfra.NewRedisConfig,
		"dragonfly": testinfra.NewDragonflyConfig,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			client, err := internalredis.New(ctx, cfg(t))
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			s := deliverystatus.NewRedisStore(client, "dep", time.Hour)

			require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{
				entry("t1", "sub_1", models.AttemptStatusFailed, "500", time.UnixMilli(999)),
				entry("t1", "sub_2", models.AttemptStatusSuccess, "200", time.UnixMilli(5000)),
			}))
			require.NoError(t, s.RecordAttempts(ctx, []*models.LogEntry{
				entry("t1", "sub_1", models.AttemptStatusSuccess, "200", time.UnixMilli(1000)),
				entry("t1", "sub_2", models.AttemptStatusFailed, "503", time.UnixMilli(4000)),
			}))

			s1, err := s.GetAttemptStatus(ctx, "t1", "sub_1")
			require.NoError(t, err)
			require.NotNil(t, s1)
			assert.Equal(t, time.UnixMilli(1000).UTC(), s1.LastAttemptAt)
			assert.Equal(t, models.AttemptStatusSuccess, s1.LastStatus)
			assert.Equal(t, time.UnixMilli(1000).UTC(), s1.LastSuccessAt)

			s2, err := s.GetAttemptStatus(ctx, "t1", "sub_2")
			require.NoError(t, err)
			require.NotNil(t, s2)
			assert.Equal(t, time.UnixMilli(5000).UTC(), s2.LastAttemptAt, "an older attempt does not overwrite")
			assert.Equal(t, "200", s2.LastCode)

			ttl, err := client.TTL(ctx, "dep:tenant:{t1}:mcp_status:sub_1").Result()
			require.NoError(t, err)
			assert.InDelta(t, time.Hour.Seconds(), ttl.Seconds(), 5)

			missing, err := s.GetAttemptStatus(ctx, "t1", "nope")
			require.NoError(t, err)
			assert.Nil(t, missing)
		})
	}
}

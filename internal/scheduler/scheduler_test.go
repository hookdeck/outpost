package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/EventKit/internal/scheduler"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestScheduler(t *testing.T) {
	t.Parallel()

	redisClient := testutil.CreateTestRedisClient(t)

	called := false
	exec := func(_ context.Context, id string, scheduledAt time.Time) error {
		called = true
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := scheduler.New(redisClient, exec)
	s.Schedule(ctx, "id", time.Now().Add(2*time.Second))
	go s.Monitor(ctx)

	time.Sleep(2 * time.Second)
	assert.False(t, called)
	time.Sleep(2 * time.Second)
	assert.True(t, called)
}

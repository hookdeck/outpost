package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/scheduler"
	"github.com/hookdeck/EventKit/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

func TestScheduler_Basic(t *testing.T) {
	t.Parallel()

	redisConfig := testutil.CreateTestRedisConfig(t)

	msgs := []string{}
	exec := func(_ context.Context, id string) error {
		msgs = append(msgs, id)
		return nil
	}

	ctx := context.Background()
	s := scheduler.New("scheduler", redisConfig, exec)
	require.NoError(t, s.Init(ctx))
	defer s.Shutdown()
	go s.Monitor(ctx)

	// Act
	ids := []string{
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
	}
	s.Schedule(ctx, ids[0], 1*time.Second)
	s.Schedule(ctx, ids[1], 2*time.Second)
	s.Schedule(ctx, ids[2], 3*time.Second)

	// Assert
	time.Sleep(time.Second / 2)
	require.Len(t, msgs, 0)
	time.Sleep(time.Second)
	require.Len(t, msgs, 1)
	require.Equal(t, ids[0], msgs[0])
	time.Sleep(time.Second)
	require.Len(t, msgs, 2)
	require.Equal(t, ids[1], msgs[1])
	time.Sleep(time.Second)
	require.Len(t, msgs, 3)
	require.Equal(t, ids[2], msgs[2])
}

func TestScheduler_ParallelMonitor(t *testing.T) {
	t.Parallel()

	redisConfig := testutil.CreateTestRedisConfig(t)

	msgs := []string{}
	exec := func(_ context.Context, id string) error {
		msgs = append(msgs, id)
		return nil
	}

	ctx := context.Background()
	s := scheduler.New("scheduler", redisConfig, exec)
	require.NoError(t, s.Init(ctx))
	defer s.Shutdown()

	go s.Monitor(ctx)
	go s.Monitor(ctx)
	go s.Monitor(ctx)

	// Act
	ids := []string{
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
	}
	s.Schedule(ctx, ids[0], 1*time.Second)
	s.Schedule(ctx, ids[1], 2*time.Second)
	s.Schedule(ctx, ids[2], 3*time.Second)

	// Assert
	time.Sleep(time.Second / 2)
	require.Len(t, msgs, 0)
	time.Sleep(time.Second)
	require.Len(t, msgs, 1)
	require.Equal(t, ids[0], msgs[0])
	time.Sleep(time.Second)
	require.Len(t, msgs, 2)
	require.Equal(t, ids[1], msgs[1])
	time.Sleep(time.Second)
	require.Len(t, msgs, 3)
	require.Equal(t, ids[2], msgs[2])
}

func TestScheduler_VisibilityTimeout(t *testing.T) {
	t.Parallel()

	redisConfig := testutil.CreateTestRedisConfig(t)

	msgs := []string{}
	exec := func(_ context.Context, id string) error {
		msgs = append(msgs, id)
		return errors.New("error")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	s := scheduler.New("scheduler", redisConfig, exec, scheduler.WithVisibilityTimeout(1))
	require.NoError(t, s.Init(ctx))
	defer s.Shutdown()

	go s.Monitor(ctx)

	id := uuid.New().String()
	s.Schedule(ctx, id, 1*time.Second)

	<-ctx.Done()
	require.Len(t, msgs, 3)
	require.Equal(t, id, msgs[0])
	require.Equal(t, id, msgs[1])
	require.Equal(t, id, msgs[2])
}

package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/hookdeck/outpost/internal/util/testutil"
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

func TestScheduler_CustomID(t *testing.T) {
	t.Parallel()

	redisConfig := testutil.CreateTestRedisConfig(t)
	ctx := context.Background()

	setupTestScheduler := func(t *testing.T) (scheduler.Scheduler, *[]string) {
		msgs := []string{}
		exec := func(_ context.Context, task string) error {
			msgs = append(msgs, task)
			return nil
		}

		s := scheduler.New(uuid.New().String(), redisConfig, exec)
		require.NoError(t, s.Init(ctx))
		go s.Monitor(ctx)

		t.Cleanup(func() {
			s.Shutdown()
		})

		return s, &msgs
	}

	t.Run("different IDs execute independently", func(t *testing.T) {
		s, msgs := setupTestScheduler(t)

		task := "test_task"
		id1 := "custom_id_1"
		id2 := "custom_id_2"

		// Schedule same task with different IDs
		require.NoError(t, s.Schedule(ctx, task, 0, scheduler.WithTaskID(id1)))
		require.NoError(t, s.Schedule(ctx, task, 0, scheduler.WithTaskID(id2)))

		time.Sleep(time.Second / 2)
		require.Len(t, *msgs, 2)
		require.Equal(t, task, (*msgs)[0])
		require.Equal(t, task, (*msgs)[1])
	})

	t.Run("same ID overrides previous task and timing", func(t *testing.T) {
		s, msgs := setupTestScheduler(t)

		id := "override_id"
		task1 := "original_task"
		task2 := "override_task"

		// Schedule first task for 1s
		require.NoError(t, s.Schedule(ctx, task1, time.Second, scheduler.WithTaskID(id)))

		// Override with second task for 2s
		require.NoError(t, s.Schedule(ctx, task2, 2*time.Second, scheduler.WithTaskID(id)))

		// At 1s mark (original task's time), nothing should execute
		time.Sleep(time.Second + 100*time.Millisecond)
		require.Empty(t, *msgs, "no task should execute at 1s")

		// At 2s mark, only the override should execute
		time.Sleep(time.Second + 100*time.Millisecond)
		require.Len(t, *msgs, 1, "override task should execute at 2s")
		require.Equal(t, task2, (*msgs)[0], "only override task should execute")
	})

	t.Run("no ID generates unique IDs", func(t *testing.T) {
		s, msgs := setupTestScheduler(t)

		task := "same_task"

		// Schedule same task multiple times without ID
		require.NoError(t, s.Schedule(ctx, task, 0))
		require.NoError(t, s.Schedule(ctx, task, 0))

		time.Sleep(time.Second / 2)
		require.Len(t, *msgs, 2)
		require.Equal(t, task, (*msgs)[0])
		require.Equal(t, task, (*msgs)[1])
	})

	t.Run("ID can be reused after task executes", func(t *testing.T) {
		s, msgs := setupTestScheduler(t)

		id := "reusable_id"
		task1 := "first_task"
		task2 := "second_task"

		// Schedule first task
		require.NoError(t, s.Schedule(ctx, task1, 100*time.Millisecond, scheduler.WithTaskID(id)))

		// Wait for first task to execute
		time.Sleep(200 * time.Millisecond)
		require.Len(t, *msgs, 1)
		require.Equal(t, task1, (*msgs)[0])

		// Schedule second task with same ID
		require.NoError(t, s.Schedule(ctx, task2, 100*time.Millisecond, scheduler.WithTaskID(id)))

		// Wait for second task to execute
		time.Sleep(200 * time.Millisecond)
		require.Len(t, *msgs, 2)
		require.Equal(t, task2, (*msgs)[1])
	})
}

func TestScheduler_Cancel(t *testing.T) {
	t.Parallel()

	redisConfig := testutil.CreateTestRedisConfig(t)
	ctx := context.Background()

	setupTestScheduler := func(t *testing.T) (scheduler.Scheduler, *[]string) {
		msgs := []string{}
		exec := func(_ context.Context, task string) error {
			msgs = append(msgs, task)
			return nil
		}

		s := scheduler.New(uuid.New().String(), redisConfig, exec)
		require.NoError(t, s.Init(ctx))
		go s.Monitor(ctx)

		t.Cleanup(func() {
			s.Shutdown()
		})

		return s, &msgs
	}

	t.Run("cancel removes scheduled task", func(t *testing.T) {
		s, msgs := setupTestScheduler(t)

		task := "task_to_cancel"
		id := "cancel_id"

		// Schedule task with 1s delay
		require.NoError(t, s.Schedule(ctx, task, time.Second, scheduler.WithTaskID(id)))

		// Cancel it immediately
		require.NoError(t, s.Cancel(ctx, id))

		// Wait past when it would have executed
		time.Sleep(time.Second + 100*time.Millisecond)
		require.Empty(t, *msgs, "cancelled task should not execute")
	})

	t.Run("cancel is idempotent", func(t *testing.T) {
		s, _ := setupTestScheduler(t)

		id := "non_existent_id"
		// Cancel non-existent task should not error
		require.NoError(t, s.Cancel(ctx, id))
		// Cancel again should still not error
		require.NoError(t, s.Cancel(ctx, id))
	})
}

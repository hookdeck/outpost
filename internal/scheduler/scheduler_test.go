package scheduler_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	iredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/rsmq"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

// mockRSMQ is a test double that wraps a real RSMQ client and injects
// transient errors into ReceiveMessage for the first failCount calls.
type mockRSMQ struct {
	inner     *rsmq.RedisSMQ
	calls     atomic.Int64
	failCount int64
	failErr   error
}

func (m *mockRSMQ) CreateQueue(qname string, vt uint, delay uint, maxsize int) error {
	return m.inner.CreateQueue(qname, vt, delay, maxsize)
}

func (m *mockRSMQ) ReceiveMessage(qname string, vt uint) (*rsmq.QueueMessage, error) {
	if m.calls.Add(1) <= m.failCount {
		return nil, m.failErr
	}
	return m.inner.ReceiveMessage(qname, vt)
}

func (m *mockRSMQ) SendMessage(qname string, message string, delay uint, opts ...rsmq.SendMessageOption) (string, error) {
	return m.inner.SendMessage(qname, message, delay, opts...)
}

func (m *mockRSMQ) DeleteMessage(qname string, id string) error {
	return m.inner.DeleteMessage(qname, id)
}

func (m *mockRSMQ) Quit() error {
	return m.inner.Quit()
}

// alwaysFailRSMQ is a test double that always fails ReceiveMessage.
type alwaysFailRSMQ struct {
	err error
}

func (m *alwaysFailRSMQ) CreateQueue(string, uint, uint, int) error { return nil }
func (m *alwaysFailRSMQ) ReceiveMessage(string, uint) (*rsmq.QueueMessage, error) {
	return nil, m.err
}
func (m *alwaysFailRSMQ) SendMessage(string, string, uint, ...rsmq.SendMessageOption) (string, error) {
	return "", nil
}
func (m *alwaysFailRSMQ) DeleteMessage(string, string) error { return nil }
func (m *alwaysFailRSMQ) Quit() error                        { return nil }

// createRSMQClient creates an RSMQ client for testing
func createRSMQClient(t *testing.T, redisConfig *iredis.RedisConfig) *rsmq.RedisSMQ {
	ctx := context.Background()
	redisClient, err := iredis.New(ctx, redisConfig)
	require.NoError(t, err)

	adapter := rsmq.NewRedisAdapter(redisClient)
	return rsmq.NewRedisSMQ(adapter, "rsmq")
}

func TestScheduler_Basic(t *testing.T) {
	t.Parallel()

	redisConfig := testutil.CreateTestRedisConfig(t)
	rsmqClient := createRSMQClient(t, redisConfig)
	logger := testutil.CreateTestLogger(t)

	msgs := []string{}
	exec := func(_ context.Context, id string) error {
		msgs = append(msgs, id)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := scheduler.New("scheduler", rsmqClient, exec, scheduler.WithLogger(logger))
	require.NoError(t, s.Init(ctx))
	defer func() { cancel(); s.Shutdown() }()
	go s.Monitor(ctx)

	// Act
	ids := []string{
		idgen.String(),
		idgen.String(),
		idgen.String(),
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
	rsmqClient := createRSMQClient(t, redisConfig)
	logger := testutil.CreateTestLogger(t)

	msgs := []string{}
	exec := func(_ context.Context, id string) error {
		msgs = append(msgs, id)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := scheduler.New("scheduler", rsmqClient, exec, scheduler.WithLogger(logger))
	require.NoError(t, s.Init(ctx))
	defer func() { cancel(); s.Shutdown() }()

	go s.Monitor(ctx)
	go s.Monitor(ctx)
	go s.Monitor(ctx)

	// Act
	ids := []string{
		idgen.String(),
		idgen.String(),
		idgen.String(),
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
	rsmqClient := createRSMQClient(t, redisConfig)
	logger := testutil.CreateTestLogger(t)

	msgs := []string{}
	exec := func(_ context.Context, id string) error {
		msgs = append(msgs, id)
		return errors.New("error")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	s := scheduler.New("scheduler", rsmqClient, exec, scheduler.WithVisibilityTimeout(1), scheduler.WithLogger(logger))
	require.NoError(t, s.Init(ctx))
	defer s.Shutdown()

	go s.Monitor(ctx)

	id := idgen.String()
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
		logger := testutil.CreateTestLogger(t)
		msgs := []string{}
		exec := func(_ context.Context, task string) error {
			msgs = append(msgs, task)
			return nil
		}

		monitorCtx, cancelMonitor := context.WithCancel(ctx)

		rsmqClient := createRSMQClient(t, redisConfig)
		s := scheduler.New(idgen.String(), rsmqClient, exec, scheduler.WithLogger(logger))
		require.NoError(t, s.Init(ctx))
		go s.Monitor(monitorCtx)

		t.Cleanup(func() {
			cancelMonitor()
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
		require.Eventually(t, func() bool {
			return len(*msgs) >= 1
		}, 2*time.Second, 50*time.Millisecond, "first task should execute")
		require.Equal(t, task1, (*msgs)[0])

		// Schedule second task with same ID
		require.NoError(t, s.Schedule(ctx, task2, 100*time.Millisecond, scheduler.WithTaskID(id)))

		// Wait for second task to execute
		require.Eventually(t, func() bool {
			return len(*msgs) >= 2
		}, 2*time.Second, 50*time.Millisecond, "second task should execute")
		require.Equal(t, task2, (*msgs)[1])
	})
}

func TestScheduler_Cancel(t *testing.T) {
	t.Parallel()

	redisConfig := testutil.CreateTestRedisConfig(t)
	ctx := context.Background()

	setupTestScheduler := func(t *testing.T) (scheduler.Scheduler, *[]string) {
		logger := testutil.CreateTestLogger(t)
		msgs := []string{}
		exec := func(_ context.Context, task string) error {
			msgs = append(msgs, task)
			return nil
		}

		monitorCtx, cancelMonitor := context.WithCancel(ctx)

		rsmqClient := createRSMQClient(t, redisConfig)
		s := scheduler.New(idgen.String(), rsmqClient, exec, scheduler.WithLogger(logger))
		require.NoError(t, s.Init(ctx))
		go s.Monitor(monitorCtx)

		t.Cleanup(func() {
			cancelMonitor()
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

func TestScheduler_MonitorRetriesTransientErrors(t *testing.T) {
	t.Parallel()

	logger := testutil.CreateTestLogger(t)

	redisConfig := testutil.CreateTestRedisConfig(t)
	realClient := createRSMQClient(t, redisConfig)

	mock := &mockRSMQ{
		inner:     realClient,
		failCount: 3,
		failErr:   errors.New("connection reset"),
	}

	msgs := []string{}
	exec := func(_ context.Context, msg string) error {
		msgs = append(msgs, msg)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := scheduler.New("scheduler", mock, exec,
		scheduler.WithPollBackoff(10*time.Millisecond),
		scheduler.WithMaxConsecutiveErrors(5),
		scheduler.WithLogger(logger),
	)
	require.NoError(t, s.Init(ctx))
	defer func() { cancel(); s.Shutdown() }()

	go s.Monitor(ctx)

	// Schedule a message — Monitor should recover after 3 transient errors and process it
	id := idgen.String()
	require.NoError(t, s.Schedule(ctx, id, 0))

	time.Sleep(time.Second)
	require.Len(t, msgs, 1)
	require.Equal(t, id, msgs[0])
}

func TestScheduler_MonitorExhaustsRetries(t *testing.T) {
	t.Parallel()

	logger := testutil.CreateTestLogger(t)

	mock := &alwaysFailRSMQ{
		err: errors.New("connection reset"),
	}

	exec := func(_ context.Context, msg string) error { return nil }

	s := scheduler.New("scheduler", mock, exec,
		scheduler.WithPollBackoff(10*time.Millisecond),
		scheduler.WithMaxConsecutiveErrors(3),
		scheduler.WithLogger(logger),
	)

	// Monitor should return an error after exhausting retries
	err := s.Monitor(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "max consecutive errors reached")
	require.Contains(t, err.Error(), "connection reset")
}

func TestScheduler_MonitorCancelsDuringBackoff(t *testing.T) {
	t.Parallel()

	logger := testutil.CreateTestLogger(t)

	mock := &alwaysFailRSMQ{
		err: errors.New("connection reset"),
	}

	exec := func(_ context.Context, msg string) error { return nil }

	s := scheduler.New("scheduler", mock, exec,
		scheduler.WithPollBackoff(10*time.Millisecond),
		scheduler.WithMaxConsecutiveErrors(10),
		scheduler.WithLogger(logger),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Monitor should return nil when context is cancelled during backoff
	err := s.Monitor(ctx)
	require.NoError(t, err, "Monitor should return nil on context cancellation")
}

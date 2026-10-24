package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/rsmq"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRSMQ is a test double that wraps a real RSMQ client and injects
// transient errors into ReceiveMessagePoll for the first failCount calls.
type mockRSMQ struct {
	inner     *rsmq.RedisSMQ
	calls     atomic.Int64
	failCount int64
	failErr   error
}

func (m *mockRSMQ) CreateQueue(qname string, vt uint, delay uint, maxsize int) error {
	return m.inner.CreateQueue(qname, vt, delay, maxsize)
}

func (m *mockRSMQ) ReceiveMessagePoll(qname string, vt uint) (rsmq.PollResult, error) {
	if m.calls.Add(1) <= m.failCount {
		return rsmq.PollResult{}, m.failErr
	}
	return m.inner.ReceiveMessagePoll(qname, vt)
}

func (m *mockRSMQ) SendMessage(qname string, message string, delay uint, opts ...rsmq.SendMessageOption) (string, error) {
	return m.inner.SendMessage(qname, message, delay, opts...)
}

func (m *mockRSMQ) ChangeMessageVisibility(qname string, id string, vt uint) error {
	return m.inner.ChangeMessageVisibility(qname, id, vt)
}

func (m *mockRSMQ) DeleteMessage(qname string, id string) error {
	return m.inner.DeleteMessage(qname, id)
}

func (m *mockRSMQ) DeleteMessageWithReceipt(qname string, id string, receipt string) error {
	return m.inner.DeleteMessageWithReceipt(qname, id, receipt)
}

func (m *mockRSMQ) ChangeMessageVisibilityWithReceipt(qname string, id string, receipt string, vt uint) error {
	return m.inner.ChangeMessageVisibilityWithReceipt(qname, id, receipt, vt)
}

func (m *mockRSMQ) RequeueMessage(qname string, id string, receipt string, delay uint) error {
	return m.inner.RequeueMessage(qname, id, receipt, delay)
}

func (m *mockRSMQ) Quit() error {
	return m.inner.Quit()
}

// immediateVisibilityRSMQ records requested visibility timeouts while making
// messages immediately visible so backoff tests do not wait in real time.
type immediateVisibilityRSMQ struct {
	rsmq.Client
	visibilityTimeouts chan uint
}

func (m *immediateVisibilityRSMQ) ChangeMessageVisibilityWithReceipt(qname string, id string, receipt string, vt uint) error {
	m.visibilityTimeouts <- vt
	return m.Client.ChangeMessageVisibilityWithReceipt(qname, id, receipt, 0)
}

// alwaysFailRSMQ is a test double that always fails ReceiveMessagePoll.
type alwaysFailRSMQ struct {
	err error
}

func (m *alwaysFailRSMQ) CreateQueue(string, uint, uint, int) error { return nil }
func (m *alwaysFailRSMQ) ReceiveMessagePoll(string, uint) (rsmq.PollResult, error) {
	return rsmq.PollResult{}, m.err
}
func (m *alwaysFailRSMQ) SendMessage(string, string, uint, ...rsmq.SendMessageOption) (string, error) {
	return "", nil
}
func (m *alwaysFailRSMQ) ChangeMessageVisibility(string, string, uint) error    { return nil }
func (m *alwaysFailRSMQ) DeleteMessage(string, string) error                    { return nil }
func (m *alwaysFailRSMQ) DeleteMessageWithReceipt(string, string, string) error { return nil }
func (m *alwaysFailRSMQ) ChangeMessageVisibilityWithReceipt(string, string, string, uint) error {
	return nil
}
func (m *alwaysFailRSMQ) RequeueMessage(string, string, string, uint) error { return nil }
func (m *alwaysFailRSMQ) Quit() error                                       { return nil }

// msgLog records messages appended by executor callbacks. The scheduler runs
// exec on the monitor's goroutines, so test-side reads race with appends
// without a lock.
type msgLog struct {
	mu   sync.Mutex
	msgs []string
}

func (l *msgLog) append(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, msg)
}

func (l *msgLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.msgs...)
}

// createRSMQClient creates an RSMQ client on an in-memory Redis. Call it
// inside a synctest bubble.
func createRSMQClient(t *testing.T) *rsmq.RedisSMQ {
	_, _, redisClient := testutil.NewSynctestRedis(t)
	adapter := rsmq.NewRedisAdapter(redisClient)
	return rsmq.NewRedisSMQ(adapter, "rsmq")
}

// advance moves the fake clock forward by d and lets every goroutine in the
// bubble settle.
func advance(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

// startMonitor runs s.Monitor on ctx in a goroutine and returns a wait
// function that blocks until it has exited. Teardown order: synctest.Wait() so
// in-flight handlers finish before ctx is canceled, cancel ctx, wait, then
// Shutdown — otherwise the monitor can log through the test logger after the
// test has finished, which the race detector flags.
func startMonitor(ctx context.Context, s scheduler.Scheduler) (wait func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Monitor(ctx)
	}()
	return func() { <-done }
}

func TestScheduler_Basic(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rsmqClient := createRSMQClient(t)
		logger := testutil.CreateTestLogger(t)

		var msgs msgLog
		exec := func(_ context.Context, id string) error {
			msgs.append(id)
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		s := scheduler.New("scheduler", rsmqClient, exec, scheduler.WithLogger(logger))
		require.NoError(t, s.Init(ctx))
		waitMonitor := startMonitor(ctx, s)
		defer func() { synctest.Wait(); cancel(); waitMonitor(); s.Shutdown() }()

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
		advance(time.Second / 2)
		require.Empty(t, msgs.snapshot())
		advance(time.Second)
		got := msgs.snapshot()
		require.Len(t, got, 1)
		require.Equal(t, ids[0], got[0])
		advance(time.Second)
		got = msgs.snapshot()
		require.Len(t, got, 2)
		require.Equal(t, ids[1], got[1])
		advance(time.Second)
		got = msgs.snapshot()
		require.Len(t, got, 3)
		require.Equal(t, ids[2], got[2])
	})
}

// TestScheduler_IdleSleepWakesOnDueMessage asserts the monitor sleeps until the
// next message is due rather than for the full poll backoff. The backoff here
// is far longer than the test, so a monitor that slept it flat would never run
// the task.
func TestScheduler_IdleSleepWakesOnDueMessage(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rsmqClient := createRSMQClient(t)
		logger := testutil.CreateTestLogger(t)

		done := make(chan time.Time, 1)
		exec := func(_ context.Context, id string) error {
			done <- time.Now()
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		s := scheduler.New("scheduler", rsmqClient, exec,
			scheduler.WithPollBackoff(time.Minute),
			scheduler.WithLogger(logger))
		require.NoError(t, s.Init(ctx))
		monitorDone := make(chan struct{})
		defer func() { synctest.Wait(); cancel(); <-monitorDone; s.Shutdown() }()

		// Schedule before the monitor starts so its first poll finds the message
		// pending and has to compute the sleep from the message's due time.
		require.NoError(t, s.Schedule(ctx, idgen.String(), 1*time.Second))
		start := time.Now()
		go func() {
			defer close(monitorDone)
			s.Monitor(ctx)
		}()

		select {
		case at := <-done:
			elapsed := at.Sub(start)
			require.Equal(t, 1*time.Second, elapsed, "task should run exactly when due")
		case <-time.After(5 * time.Second):
			t.Fatal("task was not executed; monitor slept the full poll backoff")
		}
	})
}

func TestScheduler_ParallelMonitor(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rsmqClient := createRSMQClient(t)
		logger := testutil.CreateTestLogger(t)

		var msgs msgLog
		exec := func(_ context.Context, id string) error {
			msgs.append(id)
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		s := scheduler.New("scheduler", rsmqClient, exec, scheduler.WithLogger(logger))
		require.NoError(t, s.Init(ctx))

		wait1 := startMonitor(ctx, s)
		wait2 := startMonitor(ctx, s)
		wait3 := startMonitor(ctx, s)
		defer func() {
			synctest.Wait()
			cancel()
			wait1()
			wait2()
			wait3()
			s.Shutdown()
		}()

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
		advance(time.Second / 2)
		require.Empty(t, msgs.snapshot())
		advance(time.Second)
		got := msgs.snapshot()
		require.Len(t, got, 1)
		require.Equal(t, ids[0], got[0])
		advance(time.Second)
		got = msgs.snapshot()
		require.Len(t, got, 2)
		require.Equal(t, ids[1], got[1])
		advance(time.Second)
		got = msgs.snapshot()
		require.Len(t, got, 3)
		require.Equal(t, ids[2], got[2])
	})
}

func TestScheduler_VisibilityTimeout(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rsmqClient := createRSMQClient(t)
		logger := testutil.CreateTestLogger(t)

		var msgs msgLog
		exec := func(_ context.Context, id string) error {
			msgs.append(id)
			return errors.New("error")
		}

		// Receives at 1s, 2s and 3s; the 4th would be at 4s.
		ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
		defer cancel()
		s := scheduler.New("scheduler", rsmqClient, exec, scheduler.WithVisibilityTimeout(1), scheduler.WithLogger(logger))
		require.NoError(t, s.Init(ctx))
		waitMonitor := startMonitor(ctx, s)
		defer func() { synctest.Wait(); cancel(); waitMonitor(); s.Shutdown() }()

		id := idgen.String()
		s.Schedule(ctx, id, 1*time.Second)

		<-ctx.Done()
		got := msgs.snapshot()
		require.Len(t, got, 3)
		require.Equal(t, id, got[0])
		require.Equal(t, id, got[1])
		require.Equal(t, id, got[2])
	})
}

// setupTestScheduler starts a scheduler and its monitor on a fresh in-memory
// Redis. Call it inside a synctest bubble.
func setupTestScheduler(t *testing.T) (scheduler.Scheduler, *msgLog) {
	ctx := context.Background()
	logger := testutil.CreateTestLogger(t)
	msgs := &msgLog{}
	exec := func(_ context.Context, task string) error {
		msgs.append(task)
		return nil
	}

	monitorCtx, cancelMonitor := context.WithCancel(ctx)

	rsmqClient := createRSMQClient(t)
	s := scheduler.New(idgen.String(), rsmqClient, exec, scheduler.WithLogger(logger))
	require.NoError(t, s.Init(ctx))
	waitMonitor := startMonitor(monitorCtx, s)

	t.Cleanup(func() {
		synctest.Wait()
		cancelMonitor()
		waitMonitor()
		s.Shutdown()
	})

	return s, msgs
}

func TestScheduler_CustomID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("different IDs execute independently", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, msgs := setupTestScheduler(t)

			task := "test_task"
			id1 := "custom_id_1"
			id2 := "custom_id_2"

			// Schedule same task with different IDs
			require.NoError(t, s.Schedule(ctx, task, 0, scheduler.WithTaskID(id1)))
			require.NoError(t, s.Schedule(ctx, task, 0, scheduler.WithTaskID(id2)))

			advance(time.Second / 2)
			got := msgs.snapshot()
			require.Len(t, got, 2)
			require.Equal(t, task, got[0])
			require.Equal(t, task, got[1])
		})
	})

	t.Run("same ID overrides previous task and timing", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, msgs := setupTestScheduler(t)

			id := "override_id"
			task1 := "original_task"
			task2 := "override_task"

			// Schedule first task for 1s
			require.NoError(t, s.Schedule(ctx, task1, time.Second, scheduler.WithTaskID(id)))

			// Override with second task for 2s
			require.NoError(t, s.Schedule(ctx, task2, 2*time.Second, scheduler.WithTaskID(id)))

			// At 1s mark (original task's time), nothing should execute
			advance(time.Second + 100*time.Millisecond)
			require.Empty(t, msgs.snapshot(), "no task should execute at 1s")

			// At 2s mark, only the override should execute
			advance(time.Second + 100*time.Millisecond)
			got := msgs.snapshot()
			require.Len(t, got, 1, "override task should execute at 2s")
			require.Equal(t, task2, got[0], "only override task should execute")
		})
	})

	t.Run("no ID generates unique IDs", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, msgs := setupTestScheduler(t)

			task := "same_task"

			// Schedule same task multiple times without ID
			require.NoError(t, s.Schedule(ctx, task, 0))
			require.NoError(t, s.Schedule(ctx, task, 0))

			advance(time.Second / 2)
			got := msgs.snapshot()
			require.Len(t, got, 2)
			require.Equal(t, task, got[0])
			require.Equal(t, task, got[1])
		})
	})

	t.Run("ID can be reused after task executes", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, msgs := setupTestScheduler(t)

			id := "reusable_id"
			task1 := "first_task"
			task2 := "second_task"

			// Schedule first task
			require.NoError(t, s.Schedule(ctx, task1, 100*time.Millisecond, scheduler.WithTaskID(id)))

			// First task executes
			advance(time.Second / 2)
			require.Equal(t, []string{task1}, msgs.snapshot(), "first task should execute")

			// Schedule second task with same ID
			require.NoError(t, s.Schedule(ctx, task2, 100*time.Millisecond, scheduler.WithTaskID(id)))

			// Second task executes
			advance(time.Second / 2)
			require.Equal(t, []string{task1, task2}, msgs.snapshot(), "second task should execute")
		})
	})
}

func TestScheduler_Cancel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("cancel removes scheduled task", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, msgs := setupTestScheduler(t)

			task := "task_to_cancel"
			id := "cancel_id"

			// Schedule task with 1s delay
			require.NoError(t, s.Schedule(ctx, task, time.Second, scheduler.WithTaskID(id)))

			// Cancel it immediately
			require.NoError(t, s.Cancel(ctx, id))

			// Wait past when it would have executed
			advance(time.Second + 100*time.Millisecond)
			require.Empty(t, msgs.snapshot(), "cancelled task should not execute")
		})
	})

	t.Run("cancel is idempotent", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			s, _ := setupTestScheduler(t)

			id := "non_existent_id"
			// Cancel non-existent task should not error
			require.NoError(t, s.Cancel(ctx, id))
			// Cancel again should still not error
			require.NoError(t, s.Cancel(ctx, id))
		})
	})
}

func TestScheduler_MaxReceiveCountMovesToDLQ(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rsmqClient := createRSMQClient(t)
		immediateClient := &immediateVisibilityRSMQ{
			Client:             rsmqClient,
			visibilityTimeouts: make(chan uint, 2),
		}
		logger := testutil.CreateTestLogger(t)

		task := "poison_task"
		var execCount atomic.Int64
		exec := func(_ context.Context, _ string) error {
			execCount.Add(1)
			return errors.New("permanent failure")
		}

		ctx, cancel := context.WithCancel(context.Background())
		s := scheduler.New("scheduler", immediateClient, exec,
			scheduler.WithVisibilityTimeout(1),
			scheduler.WithPollBackoff(10*time.Millisecond),
			scheduler.WithMaxReceiveCount(2),
			scheduler.WithRandForTest(func() float64 { return 0.5 }),
			scheduler.WithLogger(logger))
		require.NoError(t, s.Init(ctx))
		waitMonitor := startMonitor(ctx, s)
		defer func() { synctest.Wait(); cancel(); waitMonitor(); s.Shutdown() }()

		require.NoError(t, s.Schedule(ctx, task, 0))

		advance(time.Second)
		dlqMsg, err := rsmqClient.ReceiveMessage("scheduler-dlq", rsmq.UnsetVt)
		require.NoError(t, err)
		require.NotNil(t, dlqMsg, "message should land in the DLQ")
		require.Equal(t, task, dlqMsg.Message, "DLQ message should carry the original task")
		require.EqualValues(t, 2, execCount.Load(), "executor should run exactly maxReceiveCount times")
		require.EqualValues(t, 1, <-immediateClient.visibilityTimeouts)
		require.EqualValues(t, 2, <-immediateClient.visibilityTimeouts)

		// The message must be gone from the main queue.
		msg, err := rsmqClient.ReceiveMessage("scheduler", rsmq.UnsetVt)
		require.NoError(t, err)
		require.Nil(t, msg, "main queue should be empty after DLQ routing")
	})
}

func TestScheduler_RescheduleDuringExecSurvives(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rsmqClient := createRSMQClient(t)
		logger := testutil.CreateTestLogger(t)

		var msgs msgLog
		var s scheduler.Scheduler
		ctx, cancel := context.WithCancel(context.Background())
		exec := func(_ context.Context, task string) error {
			msgs.append(task)
			if task == "first" {
				assert.NoError(t, s.Schedule(ctx, "second", 0, scheduler.WithTaskID("task")))
			}
			return nil
		}
		s = scheduler.New(idgen.String(), rsmqClient, exec,
			scheduler.WithPollBackoff(10*time.Millisecond),
			scheduler.WithLogger(logger))
		require.NoError(t, s.Init(ctx))
		waitMonitor := startMonitor(ctx, s)
		defer func() { synctest.Wait(); cancel(); waitMonitor(); s.Shutdown() }()

		require.NoError(t, s.Schedule(ctx, "first", 0, scheduler.WithTaskID("task")))

		advance(time.Second)
		require.Equal(t, []string{"first", "second"}, msgs.snapshot())
	})
}

func TestScheduler_RescheduleDuringFailedExecKeepsDelay(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rsmqClient := createRSMQClient(t)
		logger := testutil.CreateTestLogger(t)

		var msgs msgLog
		var s scheduler.Scheduler
		ctx, cancel := context.WithCancel(context.Background())
		exec := func(_ context.Context, task string) error {
			msgs.append(task)
			if task == "first" {
				assert.NoError(t, s.Schedule(ctx, "second", 0, scheduler.WithTaskID("task")))
				return errors.New("failure")
			}
			return nil
		}
		s = scheduler.New(idgen.String(), rsmqClient, exec,
			scheduler.WithVisibilityTimeout(60),
			scheduler.WithPollBackoff(10*time.Millisecond),
			scheduler.WithMaxReceiveCount(5),
			scheduler.WithLogger(logger))
		require.NoError(t, s.Init(ctx))
		waitMonitor := startMonitor(ctx, s)
		defer func() { synctest.Wait(); cancel(); waitMonitor(); s.Shutdown() }()

		require.NoError(t, s.Schedule(ctx, "first", 0, scheduler.WithTaskID("task")))

		advance(3 * time.Second) // well under the failed run's 60s backoff
		require.Equal(t, []string{"first", "second"}, msgs.snapshot())
	})
}

func TestScheduler_MonitorRetriesTransientErrors(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		logger := testutil.CreateTestLogger(t)

		realClient := createRSMQClient(t)

		mock := &mockRSMQ{
			inner:     realClient,
			failCount: 3,
			failErr:   errors.New("connection reset"),
		}

		var msgs msgLog
		exec := func(_ context.Context, msg string) error {
			msgs.append(msg)
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		s := scheduler.New("scheduler", mock, exec,
			scheduler.WithPollBackoff(10*time.Millisecond),
			scheduler.WithMaxConsecutiveErrors(5),
			scheduler.WithLogger(logger),
		)
		require.NoError(t, s.Init(ctx))
		waitMonitor := startMonitor(ctx, s)
		defer func() { synctest.Wait(); cancel(); waitMonitor(); s.Shutdown() }()

		// Schedule a message — Monitor should recover after 3 transient errors and process it
		id := idgen.String()
		require.NoError(t, s.Schedule(ctx, id, 0))

		// 3 errors back off 100ms + 200ms + 400ms before the message is received.
		advance(600 * time.Millisecond)
		require.Empty(t, msgs.snapshot(), "message must wait out the error backoffs")
		advance(400 * time.Millisecond)
		require.Len(t, msgs.snapshot(), 1)
		require.Equal(t, id, msgs.snapshot()[0])
	})
}

func TestScheduler_MonitorExhaustsRetries(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
	})
}

func TestScheduler_MonitorCancelsDuringBackoff(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
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
	})
}

// Monitor can be called again on the same scheduler after it returned an
// error (as when the supervisor restarts the retry worker): the next run
// picks up due messages and runs each once.
func TestScheduler_MonitorRerunAfterError(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rsmqClient := &mockRSMQ{
			inner:     createRSMQClient(t),
			failCount: 1,
			failErr:   errors.New("connection refused"),
		}
		logger := testutil.CreateTestLogger(t)

		var msgs msgLog
		exec := func(_ context.Context, id string) error {
			msgs.append(id)
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		s := scheduler.New("scheduler", rsmqClient, exec,
			scheduler.WithLogger(logger),
			scheduler.WithMaxConsecutiveErrors(1),
		)
		require.NoError(t, s.Init(ctx))

		ids := []string{idgen.String(), idgen.String()}
		require.NoError(t, s.Schedule(ctx, ids[0], 0))
		require.Error(t, s.Monitor(ctx), "first run ends on the poll error")
		require.Empty(t, msgs.snapshot())

		waitMonitor := startMonitor(ctx, s)
		defer func() { synctest.Wait(); cancel(); waitMonitor(); s.Shutdown() }()
		require.NoError(t, s.Schedule(ctx, ids[1], 0))

		advance(time.Second)
		require.ElementsMatch(t, ids, msgs.snapshot())
		advance(300 * time.Millisecond)
		require.ElementsMatch(t, ids, msgs.snapshot(), "each message runs once")
	})
}

func TestScheduler_RetryLaterDoesNotCountReceives(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		rsmqClient := createRSMQClient(t)
		logger := testutil.CreateTestLogger(t)

		var execCount atomic.Int64
		exec := func(_ context.Context, _ string) error {
			if execCount.Add(1) <= 4 {
				return scheduler.RetryLater(0, errors.New("not ready"))
			}
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		s := scheduler.New("scheduler", rsmqClient, exec,
			scheduler.WithVisibilityTimeout(1),
			scheduler.WithPollBackoff(10*time.Millisecond),
			scheduler.WithMaxReceiveCount(2),
			scheduler.WithRandForTest(func() float64 { return 0.5 }),
			scheduler.WithLogger(logger))
		require.NoError(t, s.Init(ctx))
		waitMonitor := startMonitor(ctx, s)
		defer func() { synctest.Wait(); cancel(); waitMonitor(); s.Shutdown() }()

		require.NoError(t, s.Schedule(ctx, "task", 0))

		advance(time.Second)
		require.EqualValues(t, 5, execCount.Load())
		dlqMsg, err := rsmqClient.ReceiveMessage("scheduler-dlq", rsmq.UnsetVt)
		require.NoError(t, err)
		require.Nil(t, dlqMsg, "not-ready receives must not reach the DLQ")
	})
}

package scheduler_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

func TestScheduler_MaxConcurrencyBoundsReceives(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const limit, total = 3, 10

		rsmqClient := createRSMQClient(t)
		logger := testutil.CreateTestLogger(t)

		var inFlight, maxInFlight, executed atomic.Int64
		release := make(chan struct{})
		var releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		exec := func(_ context.Context, _ string) error {
			n := inFlight.Add(1)
			for {
				m := maxInFlight.Load()
				if n <= m || maxInFlight.CompareAndSwap(m, n) {
					break
				}
			}
			<-release
			inFlight.Add(-1)
			executed.Add(1)
			return nil
		}

		ctx, cancel := context.WithCancel(context.Background())
		s := scheduler.New("scheduler", rsmqClient, exec,
			scheduler.WithPollBackoff(10*time.Millisecond),
			scheduler.WithMaxConcurrency(limit),
			scheduler.WithLogger(logger))
		require.NoError(t, s.Init(ctx))
		// Two monitors share the one limit.
		wait1 := startMonitor(ctx, s)
		wait2 := startMonitor(ctx, s)
		defer func() {
			unblock()
			synctest.Wait()
			cancel()
			wait1()
			wait2()
			s.Shutdown()
		}()

		for i := 0; i < total; i++ {
			require.NoError(t, s.Schedule(ctx, idgen.String(), 0))
		}

		synctest.Wait()
		// With the executions blocked, nothing beyond the limit may be received.
		advance(300 * time.Millisecond)
		attrs, err := rsmqClient.GetQueueAttributes("scheduler")
		require.NoError(t, err)
		require.EqualValues(t, limit, attrs.TotalRecv)
		require.EqualValues(t, limit, inFlight.Load())

		unblock()
		synctest.Wait()
		require.EqualValues(t, total, executed.Load())
		require.EqualValues(t, limit, maxInFlight.Load())
	})
}

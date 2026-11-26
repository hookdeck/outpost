package idempotence_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newIdempotence builds an Idempotence on an in-memory Redis. Call it inside a
// synctest bubble.
func newIdempotence(t *testing.T, opts ...func(*idempotence.IdempotenceImplOptions)) idempotence.Idempotence {
	_, _, client := testutil.NewSynctestRedis(t)
	return idempotence.New(client, opts...)
}

// runInBubble runs fn as a parallel subtest on a fake clock.
func runInBubble(t *testing.T, name string, fn func(t *testing.T)) {
	t.Run(name, func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, fn)
	})
}

// The counter is atomic: countexec increments it from its own goroutine while
// the test body reads it.
func setupCountExec(_ *testing.T, ctx context.Context, timeout time.Duration, ex func() error) (exec func(context.Context) error, countexec func(count *atomic.Int64), cleanup func()) {
	execchan := make(chan struct{}, 10) // buffered to prevent blocking senders
	exec = func(_ context.Context) error {
		time.Sleep(timeout)
		select {
		case execchan <- struct{}{}:
		default: // don't block if channel is full
		}
		return ex()
	}
	cleanup = func() {
		// no-op: let channel be garbage collected
	}
	countexec = func(count *atomic.Int64) {
		for {
			select {
			case <-execchan:
				count.Add(1)
			case <-ctx.Done():
				return
			}
		}
	}
	return exec, countexec, cleanup
}

func TestIdempotence_Success(t *testing.T) {
	t.Parallel()

	opts := []func(*idempotence.IdempotenceImplOptions){
		idempotence.WithTimeout(1 * time.Second),
		idempotence.WithSuccessfulTTL(24 * time.Hour),
	}

	runInBubble(t, "on separate keys", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		exec, countexec, cleanup := setupCountExec(t, ctx, 0, func() error { return nil })
		defer cleanup()
		go func() {
			i.Exec(ctx, "1", exec)
		}()
		go func() {
			i.Exec(ctx, "2", exec)
		}()
		var count atomic.Int64
		go countexec(&count)
		synctest.Wait()
		assert.EqualValues(t, 2, count.Load(), "should execute twice")
		cancel()
	})

	runInBubble(t, "when 2nd exec is within processing window", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		exec, countexec, cleanup := setupCountExec(t, ctx, 200*time.Millisecond, func() error { return nil })
		defer cleanup()
		key := testutil.RandomString(5)
		go func() {
			i.Exec(ctx, key, exec) // 1st exec
		}()
		synctest.Wait() // 1st exec is running
		errchan := make(chan error, 1)
		go func() {
			time.Sleep(100 * time.Millisecond)
			// A concurrent claim makes Exec wait the full Timeout (1s) before checking status.
			errchan <- i.Exec(ctx, key, exec) // 2nd exec
		}()
		var count atomic.Int64
		go countexec(&count)
		var err error
		select {
		case err = <-errchan:
		case <-time.After(2 * time.Second):
			require.Fail(t, "timeout waiting for 2nd exec")
		}
		synctest.Wait()
		assert.Nil(t, err, "should not return error")
		assert.EqualValues(t, 1, count.Load(), "should execute once")
		cancel()
	})

	runInBubble(t, "when 2nd exec is after processed", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		exec, countexec, cleanup := setupCountExec(t, ctx, 200*time.Millisecond, func() error { return nil })
		defer cleanup()
		key := testutil.RandomString(5)
		go func() {
			i.Exec(ctx, key, exec) // 1st exec
		}()
		errchan := make(chan error, 1)
		go func() {
			time.Sleep(500 * time.Millisecond) // wait for 1st exec to finish
			errchan <- i.Exec(ctx, key, exec)  // 2nd exec
		}()
		var count atomic.Int64
		go countexec(&count)
		var err error
		select {
		case err = <-errchan:
		case <-time.After(2 * time.Second):
			require.Fail(t, "timeout waiting for 2nd exec")
		}
		synctest.Wait()
		assert.Nil(t, err, "should not return error")
		assert.EqualValues(t, 1, count.Load(), "should execute once")
		cancel()
	})
}

func TestIdempotence_WithDeploymentID(t *testing.T) {
	t.Parallel()

	opts := []func(*idempotence.IdempotenceImplOptions){
		idempotence.WithTimeout(1 * time.Second),
		idempotence.WithSuccessfulTTL(24 * time.Hour),
		idempotence.WithDeploymentID("dp_test_001"),
	}

	runInBubble(t, "executes successfully with deployment prefix", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		executed := false
		err := i.Exec(ctx, "test-key-1", func(ctx context.Context) error {
			executed = true
			return nil
		})

		assert.NoError(t, err)
		assert.True(t, executed, "function should have been executed")
	})

	runInBubble(t, "returns nil on duplicate execution with deployment prefix", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		key := testutil.RandomString(5)
		execCount := 0

		// First execution
		err := i.Exec(ctx, key, func(ctx context.Context) error {
			execCount++
			return nil
		})
		assert.NoError(t, err)

		// Second execution with same key should be idempotent
		err = i.Exec(ctx, key, func(ctx context.Context) error {
			execCount++
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 1, execCount, "function should only execute once")
	})
}

func TestIdempotence_SplitPhase(t *testing.T) {
	t.Parallel()

	opts := []func(*idempotence.IdempotenceImplOptions){
		idempotence.WithTimeout(1 * time.Second),
		idempotence.WithSuccessfulTTL(24 * time.Hour),
	}

	runInBubble(t, "unseen key is not processed", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		processed, err := i.Processed(ctx, testutil.RandomString(5))
		require.NoError(t, err)
		assert.False(t, processed)
	})

	runInBubble(t, "MarkProcessed makes the key processed", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		key := testutil.RandomString(5)
		require.NoError(t, i.MarkProcessed(ctx, key))

		processed, err := i.Processed(ctx, key)
		require.NoError(t, err)
		assert.True(t, processed)
	})

	runInBubble(t, "successful Exec marks the key processed", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		key := testutil.RandomString(5)
		require.NoError(t, i.Exec(ctx, key, func(ctx context.Context) error { return nil }))

		processed, err := i.Processed(ctx, key)
		require.NoError(t, err)
		assert.True(t, processed)
	})

	runInBubble(t, "MarkProcessed makes Exec skip", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		key := testutil.RandomString(5)
		require.NoError(t, i.MarkProcessed(ctx, key))

		executed := false
		require.NoError(t, i.Exec(ctx, key, func(ctx context.Context) error {
			executed = true
			return nil
		}))
		assert.False(t, executed, "Exec should skip a key marked processed")
	})

	runInBubble(t, "in-flight Exec claim does not count as processed", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		key := testutil.RandomString(5)
		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- i.Exec(ctx, key, func(ctx context.Context) error {
				close(entered)
				<-release
				return nil
			})
		}()

		<-entered
		processed, err := i.Processed(ctx, key)
		require.NoError(t, err)
		assert.False(t, processed, "a processing claim is not processed")

		close(release)
		require.NoError(t, <-done)
	})

	runInBubble(t, "deployment IDs scope split-phase keys", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, _, redisClient := testutil.NewSynctestRedis(t)
		iA := idempotence.New(redisClient, idempotence.WithDeploymentID("dp_a"))
		iB := idempotence.New(redisClient, idempotence.WithDeploymentID("dp_b"))

		key := testutil.RandomString(5)
		require.NoError(t, iA.MarkProcessed(ctx, key))

		processed, err := iB.Processed(ctx, key)
		require.NoError(t, err)
		assert.False(t, processed, "marks must not leak across deployments")

		processed, err = iA.Processed(ctx, key)
		require.NoError(t, err)
		assert.True(t, processed)
	})
}

func TestIdempotenceIsolation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		_, _, redisClient := testutil.NewSynctestRedis(t)

		// Create two idempotence instances with different deployment IDs
		i1 := idempotence.New(redisClient,
			idempotence.WithTimeout(1*time.Second),
			idempotence.WithSuccessfulTTL(24*time.Hour),
			idempotence.WithDeploymentID("dp_001"),
		)
		i2 := idempotence.New(redisClient,
			idempotence.WithTimeout(1*time.Second),
			idempotence.WithSuccessfulTTL(24*time.Hour),
			idempotence.WithDeploymentID("dp_002"),
		)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Use the same key for both
		sharedKey := "shared-idempotency-key"

		exec1Count := 0
		exec2Count := 0

		// Execute on instance 1
		err := i1.Exec(ctx, sharedKey, func(ctx context.Context) error {
			exec1Count++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, exec1Count)

		// Execute on instance 2 with same key - should execute (different deployment)
		err = i2.Exec(ctx, sharedKey, func(ctx context.Context) error {
			exec2Count++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, exec2Count, "Instance 2 should execute independently")

		// Execute on instance 1 again - should be idempotent
		err = i1.Exec(ctx, sharedKey, func(ctx context.Context) error {
			exec1Count++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, exec1Count, "Instance 1 should remain idempotent")

		// Execute on instance 2 again - should be idempotent
		err = i2.Exec(ctx, sharedKey, func(ctx context.Context) error {
			exec2Count++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, exec2Count, "Instance 2 should remain idempotent")
	})
}

func TestIdempotence_Failure(t *testing.T) {
	t.Parallel()

	errExec := errors.New("exec error")

	opts := []func(*idempotence.IdempotenceImplOptions){
		idempotence.WithTimeout(1 * time.Second),
		idempotence.WithSuccessfulTTL(24 * time.Hour),
	}

	runInBubble(t, "when 2nd exec is within processing window", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		exec, countexec, cleanup := setupCountExec(t, ctx, 200*time.Millisecond, func() error { return errExec })
		defer cleanup()

		key := testutil.RandomString(5)
		err1chan := make(chan error, 1)
		err2chan := make(chan error, 1)
		go func() {
			err1chan <- i.Exec(ctx, key, exec) // 1st exec
		}()
		synctest.Wait() // 1st exec is running
		go func() {
			err2chan <- i.Exec(ctx, key, exec) // 2nd exec
		}()

		var err1, err2 error
		select {
		case err1 = <-err1chan:
		case <-time.After(2 * time.Second):
			require.Fail(t, "timeout waiting for err1")
		}
		select {
		case err2 = <-err2chan:
		case <-time.After(2 * time.Second):
			require.Fail(t, "timeout waiting for err2")
		}
		var count atomic.Int64
		go countexec(&count)
		synctest.Wait()
		cancel()
		assert.Equal(t, errExec, err1, "first execution should return exec error")
		assert.Equal(t, idempotence.ErrConflict, err2, "second execution should return conflict error")
		assert.EqualValues(t, 1, count.Load(), "should execute once")
	})

	runInBubble(t, "when 2nd exec is after 1st exec completion", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		exec, countexec, cleanup := setupCountExec(t, ctx, 100*time.Millisecond, func() error { return errExec })
		defer cleanup()
		key := testutil.RandomString(5)
		err1 := i.Exec(ctx, key, exec) // 1st exec
		err2 := i.Exec(ctx, key, exec) // 2nd exec (after 1st completed)
		var count atomic.Int64
		go countexec(&count)
		synctest.Wait()
		cancel()
		assert.Equal(t, errExec, err1, "first execution should return exec error")
		assert.Equal(t, errExec, err2, "second execution should return exec error")
		assert.EqualValues(t, 2, count.Load(), "should execute twice")
	})
}

func TestIdempotence_PanicClearsKey(t *testing.T) {
	t.Parallel()

	opts := []func(*idempotence.IdempotenceImplOptions){
		idempotence.WithTimeout(time.Minute),
		idempotence.WithSuccessfulTTL(24 * time.Hour),
	}

	runInBubble(t, "an immediate re-run executes and the panic propagates", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		key := testutil.RandomString(5)

		assert.PanicsWithValue(t, "exec blew up", func() {
			_ = i.Exec(context.Background(), key, func(context.Context) error {
				panic("exec blew up")
			})
		})

		runs := 0
		err := i.Exec(context.Background(), key, func(context.Context) error {
			runs++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, runs, "the claim was cleared, so the redelivery ran instead of conflicting")

		processed, err := i.Processed(context.Background(), key)
		require.NoError(t, err)
		assert.True(t, processed)
	})

	runInBubble(t, "the key is cleared even when the context is canceled", func(t *testing.T) {
		i := newIdempotence(t, opts...)
		key := testutil.RandomString(5)
		ctx, cancel := context.WithCancel(context.Background())

		assert.Panics(t, func() {
			_ = i.Exec(ctx, key, func(context.Context) error {
				cancel()
				panic("exec blew up")
			})
		})

		runs := 0
		err := i.Exec(context.Background(), key, func(context.Context) error {
			runs++
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 1, runs)
	})
}

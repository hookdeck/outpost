package clickhouse

import (
	"context"
	"errors"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnOpenFailureRetriedOnNextCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		o := &fakeOpener{}
		c, err := newConn(o.open, time.Minute)
		require.NoError(t, err)
		defer c.Close()
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		require.True(t, o.get(0).closed.Load())

		boom := errors.New("open failed")
		o.setOpenErr(boom)
		assert.ErrorIs(t, c.Ping(ctx), boom)
		o.setOpenErr(nil)
		require.NoError(t, c.Ping(ctx))
		assert.Equal(t, 2, o.count())
	})
}

func TestConnIdleClose(t *testing.T) {
	ctx := context.Background()
	const idle = time.Minute

	t.Run("closes after the period and reconnects on the next call", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			o := &fakeOpener{}
			c, err := newConn(o.open, idle)
			require.NoError(t, err)
			defer c.Close()

			require.NoError(t, c.Ping(ctx))
			time.Sleep(idle - time.Second)
			synctest.Wait()
			assert.False(t, o.get(0).closed.Load())
			time.Sleep(time.Second)
			synctest.Wait()
			assert.True(t, o.get(0).closed.Load())

			require.NoError(t, c.Ping(ctx))
			assert.Equal(t, 2, o.count())

			time.Sleep(idle)
			synctest.Wait()
			assert.True(t, o.get(1).closed.Load(), "timer re-arms after a reopen")
		})
	})

	t.Run("not closed while calls keep arriving", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			o := &fakeOpener{}
			c, err := newConn(o.open, idle)
			require.NoError(t, err)
			defer c.Close()

			for range 10 {
				time.Sleep(idle / 2)
				require.NoError(t, c.Ping(ctx))
			}
			assert.Equal(t, 1, o.count())
			assert.False(t, o.get(0).closed.Load())
		})
	})

	t.Run("closes one period after the last call even if a call spans a tick", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			o := &fakeOpener{}
			c, err := newConn(o.open, idle)
			require.NoError(t, err)
			defer c.Close()

			o.get(0).block = make(chan struct{})
			done := make(chan error)
			go func() { done <- c.Ping(ctx) }()
			time.Sleep(idle + idle/2)
			synctest.Wait()
			assert.False(t, o.get(0).closed.Load(), "not closed while a call is in flight")
			close(o.get(0).block)
			require.NoError(t, <-done)

			time.Sleep(idle - time.Second)
			synctest.Wait()
			assert.False(t, o.get(0).closed.Load())
			time.Sleep(2 * time.Second)
			synctest.Wait()
			assert.True(t, o.get(0).closed.Load())
			assert.Equal(t, 1, o.count())
		})
	})
}

func TestConnTimeoutResetGuard(t *testing.T) {
	timeoutErr := &net.OpError{Op: "read", Err: timeoutNetErr{}}

	t.Run("no reset when another call on the pool succeeded since the call started", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			o := &fakeOpener{}
			c, err := newConn(o.open, -1)
			require.NoError(t, err)
			defer c.Close()
			pooled := o.get(0)
			release := make(chan struct{})
			pooled.holdFirst.Store(&release)
			pooled.failAfter.Store(ptr[error](timeoutErr))

			done := make(chan error)
			go func() { done <- c.Ping(context.Background()) }()
			synctest.Wait()

			time.Sleep(time.Second)
			require.NoError(t, c.Ping(context.Background()))

			close(release)
			assert.ErrorIs(t, <-done, timeoutErr)
			assert.Equal(t, 1, o.count())
			assert.False(t, pooled.closed.Load())
		})
	})

	t.Run("reset when nothing succeeded", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			o := &fakeOpener{errs: []error{timeoutErr}}
			c, err := newConn(o.open, -1)
			require.NoError(t, err)
			defer c.Close()

			assert.ErrorIs(t, c.Ping(context.Background()), timeoutErr)
			assert.Equal(t, 2, o.count())
			assert.EqualValues(t, 0, o.get(1).calls.Load())
			assert.True(t, o.get(0).closed.Load())
			require.NoError(t, c.Ping(context.Background()), "next call uses the new pool")
		})
	})

	t.Run("caller deadline while other calls succeed does not reset", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			o := &fakeOpener{}
			c, err := newConn(o.open, -1)
			require.NoError(t, err)
			defer c.Close()
			pooled := o.get(0)
			release := make(chan struct{})
			pooled.holdFirst.Store(&release)
			pooled.failAfter.Store(ptr[error](context.DeadlineExceeded))

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan error)
			go func() { done <- c.Ping(ctx) }()
			synctest.Wait()

			time.Sleep(time.Second)
			require.NoError(t, c.Ping(context.Background()))
			time.Sleep(5 * time.Second)

			close(release)
			assert.ErrorIs(t, <-done, context.DeadlineExceeded)
			assert.Equal(t, 1, o.count())
			assert.False(t, pooled.closed.Load())
		})
	})

	t.Run("caller deadline with no success resets without retry", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			o := &fakeOpener{errs: []error{context.DeadlineExceeded}}
			c, err := newConn(o.open, -1)
			require.NoError(t, err)
			defer c.Close()

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			time.Sleep(2 * time.Second)
			assert.ErrorIs(t, c.Ping(ctx), context.DeadlineExceeded)
			assert.Equal(t, 2, o.count())
			assert.EqualValues(t, 0, o.get(1).calls.Load())
		})
	})
}

func ptr[T any](v T) *T { return &v }

func TestConnHooksResetOnTimeoutOnlyWhenPoolUnhealthy(t *testing.T) {
	timeoutErr := &net.OpError{Op: "read", Err: timeoutNetErr{}}
	ctx := context.Background()

	cases := map[string]func(t *testing.T, c *conn, o *fakeOpener) error{
		"rows close": func(t *testing.T, c *conn, o *fakeOpener) error {
			rows, err := c.Query(ctx, "q")
			require.NoError(t, err)
			rows.(*watchedRows).Rows = errRows{err: timeoutErr}
			time.Sleep(time.Second)
			return rows.Close()
		},
		"batch send": func(t *testing.T, c *conn, o *fakeOpener) error {
			o.get(0).sendErr = timeoutErr
			b, err := c.PrepareBatch(ctx, "insert")
			require.NoError(t, err)
			time.Sleep(time.Second)
			return b.Send()
		},
	}
	for name, run := range cases {
		t.Run(name+" resets when nothing else succeeded", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				o := &fakeOpener{}
				c, err := newConn(o.open, -1)
				require.NoError(t, err)
				defer c.Close()
				assert.Error(t, run(t, c, o))
				assert.Equal(t, 2, o.count())
			})
		})
	}
	for name, run := range cases {
		t.Run(name+" keeps the pool when another call succeeded", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				o := &fakeOpener{}
				c, err := newConn(o.open, -1)
				require.NoError(t, err)
				defer c.Close()
				done := make(chan error)
				go func() { done <- run(t, c, o) }()
				time.Sleep(500 * time.Millisecond)
				require.NoError(t, c.Ping(ctx))
				assert.Error(t, <-done)
				assert.Equal(t, 1, o.count())
			})
		})
	}
}

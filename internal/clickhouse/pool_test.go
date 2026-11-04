package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeConn struct {
	driver.Conn
	id     int
	err    error
	calls  atomic.Int32
	closed atomic.Bool
	block  chan struct{}
	// holdFirst blocks only the first call.
	holdFirst atomic.Pointer[chan struct{}]
	// failAfter is returned by the call that was held once released.
	failAfter atomic.Pointer[error]
	// sendErr is returned by Send on batches this conn prepares.
	sendErr error
}

func (f *fakeConn) result() error {
	f.calls.Add(1)
	if f.block != nil {
		<-f.block
	}
	if ch := f.holdFirst.Swap(nil); ch != nil {
		<-*ch
		if e := f.failAfter.Load(); e != nil {
			return *e
		}
	}
	return f.err
}

func (f *fakeConn) Ping(context.Context) error { return f.result() }
func (f *fakeConn) Exec(context.Context, string, ...any) error {
	return f.result()
}
func (f *fakeConn) Select(context.Context, any, string, ...any) error {
	return f.result()
}
func (f *fakeConn) Query(context.Context, string, ...any) (driver.Rows, error) {
	if err := f.result(); err != nil {
		return nil, err
	}
	return fakeRows{}, nil
}
func (f *fakeConn) QueryRow(context.Context, string, ...any) driver.Row {
	return fakeRow{err: f.result()}
}
func (f *fakeConn) PrepareBatch(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error) {
	if err := f.result(); err != nil {
		return nil, err
	}
	return &fakeBatch{sendErr: f.sendErr}, nil
}
func (f *fakeConn) QueryFormat(context.Context, string, string, ...any) (io.ReadCloser, error) {
	if err := f.result(); err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader("x")), nil
}
func (f *fakeConn) InsertFormat(context.Context, string, string, io.Reader) error {
	return f.result()
}
func (f *fakeConn) Close() error { f.closed.Store(true); return nil }

type fakeRows struct{ driver.Rows }

func (fakeRows) Close() error { return nil }
func (fakeRows) Err() error   { return nil }

type fakeRow struct {
	driver.Row
	err error
}

func (r fakeRow) Err() error        { return r.err }
func (r fakeRow) Scan(...any) error { return r.err }

type fakeBatch struct {
	driver.Batch
	sendErr error
}

func (b *fakeBatch) Send() error  { return b.sendErr }
func (b *fakeBatch) Abort() error { return nil }
func (b *fakeBatch) Close() error { return nil }

type fakeOpener struct {
	mu       sync.Mutex
	conns    []*fakeConn
	errs     []error // error for the n-th opened conn
	openErr  error
	openCall int
}

func (o *fakeOpener) open() (driver.Conn, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.openCall++
	if o.openErr != nil {
		return nil, o.openErr
	}
	f := &fakeConn{id: len(o.conns)}
	if f.id < len(o.errs) {
		f.err = o.errs[f.id]
	}
	o.conns = append(o.conns, f)
	return f, nil
}

func (o *fakeOpener) setOpenErr(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.openErr = err
}

func (o *fakeOpener) get(i int) *fakeConn {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.conns[i]
}

func (o *fakeOpener) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.conns)
}

func newTestConn(t *testing.T, o *fakeOpener, idle time.Duration) *conn {
	t.Helper()
	c, err := newConn(o.open, idle)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "i/o timeout" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return true }

func TestConnReadRetriesOnceOnNewPool(t *testing.T) {
	ctx := context.Background()
	for name, call := range map[string]func(c *conn) error{
		"ping":   func(c *conn) error { return c.Ping(ctx) },
		"select": func(c *conn) error { return c.Select(ctx, nil, "q") },
		"query": func(c *conn) error {
			r, err := c.Query(ctx, "q")
			if err == nil {
				err = r.Close()
			}
			return err
		},
		"query row": func(c *conn) error {
			var v int
			return c.QueryRow(ctx, "q").Scan(&v)
		},
	} {
		t.Run(name, func(t *testing.T) {
			o := &fakeOpener{errs: []error{io.EOF}}
			c := newTestConn(t, o, -1)

			require.NoError(t, call(c))
			assert.Equal(t, 2, o.count())
			assert.EqualValues(t, 1, o.get(0).calls.Load())
			assert.EqualValues(t, 1, o.get(1).calls.Load())
			assert.True(t, o.get(0).closed.Load())
			assert.False(t, o.get(1).closed.Load())
		})
	}
}

func TestConnDialErrorNotRetried(t *testing.T) {
	err := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	o := &fakeOpener{errs: []error{err}}
	c := newTestConn(t, o, -1)
	assert.ErrorIs(t, c.Ping(context.Background()), err)
	assert.Equal(t, 1, o.count())
	assert.EqualValues(t, 1, o.get(0).calls.Load())
}

func TestConnReadRetryFailureIsReturned(t *testing.T) {
	o := &fakeOpener{errs: []error{io.EOF, io.EOF}}
	c := newTestConn(t, o, -1)
	assert.ErrorIs(t, c.Ping(context.Background()), io.EOF)
	assert.Equal(t, 3, o.count(), "second failure replaces the pool again, without a third attempt")
	assert.EqualValues(t, 1, o.get(1).calls.Load())
	assert.EqualValues(t, 0, o.get(2).calls.Load())
}

func TestConnWriteResetsWithoutRetry(t *testing.T) {
	ctx := context.Background()
	o := &fakeOpener{errs: []error{io.EOF}}
	c := newTestConn(t, o, -1)

	assert.ErrorIs(t, c.Exec(ctx, "insert"), io.EOF)
	assert.Equal(t, 2, o.count())
	assert.EqualValues(t, 1, o.get(0).calls.Load())
	assert.EqualValues(t, 0, o.get(1).calls.Load())
	assert.True(t, o.get(0).closed.Load())

	require.NoError(t, c.Exec(ctx, "insert"))
	assert.EqualValues(t, 1, o.get(1).calls.Load())
}

func TestConnNoResetOrRetry(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{"server exception", context.Background(), &clickhouse.Exception{Code: 62, Message: "syntax"}},
		{"caller canceled", canceled, io.EOF},
		{"unrelated error", context.Background(), errors.New("boom")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &fakeOpener{errs: []error{tt.err}}
			c := newTestConn(t, o, -1)
			assert.ErrorIs(t, c.Ping(tt.ctx), tt.err)
			assert.Equal(t, 1, o.count())
			assert.EqualValues(t, 1, o.get(0).calls.Load())
			assert.False(t, o.get(0).closed.Load())
		})
	}
}

func TestConnConcurrentFailuresSwapOnce(t *testing.T) {
	const n = 20
	o := &fakeOpener{errs: []error{io.EOF}}
	c := newTestConn(t, o, -1)
	first := o.get(0)
	first.block = make(chan struct{})

	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, c.Ping(context.Background()))
		}()
	}
	require.Eventually(t, func() bool { return first.calls.Load() == n }, time.Second, time.Millisecond)
	close(first.block)
	wg.Wait()

	assert.Equal(t, 2, o.count())
	assert.True(t, first.closed.Load())
}

func TestConnClose(t *testing.T) {
	o := &fakeOpener{}
	c := newTestConn(t, o, 5*time.Minute)
	require.NoError(t, c.Close())
	require.NoError(t, c.Close())
	assert.True(t, o.get(0).closed.Load())
	assert.ErrorIs(t, c.Ping(context.Background()), clickhouse.ErrConnectionClosed)
	assert.Equal(t, 1, o.count())
}

func TestClassify(t *testing.T) {
	bg := context.Background()
	canceled, cancel := context.WithCancel(bg)
	cancel()
	expired, cancelExpired := context.WithDeadline(bg, time.Now().Add(-time.Second))
	cancelExpired()
	future, cancelFuture := context.WithTimeout(bg, time.Hour)
	defer cancelFuture()
	timeoutErr := &net.OpError{Op: "read", Err: timeoutNetErr{}}

	tests := []struct {
		name  string
		ctx   context.Context
		err   error
		want  verdict
		retry bool
	}{
		{"nil", bg, nil, noAction, false},
		{"eof", bg, io.EOF, connDropped, true},
		{"wrapped eof", bg, fmt.Errorf("failed to read first block packet: %w", io.EOF), connDropped, true},
		{"unexpected eof", bg, io.ErrUnexpectedEOF, connDropped, true},
		{"reset", bg, &net.OpError{Op: "read", Err: syscall.ECONNRESET}, connDropped, true},
		{"epipe", bg, &net.OpError{Op: "write", Err: syscall.EPIPE}, connDropped, true},
		{"net timeout", bg, timeoutErr, timedOut, false},
		{"live deadline ctx", future, io.EOF, connDropped, true},
		{"server exception", bg, &clickhouse.Exception{Code: 62, Message: "syntax"}, noAction, false},
		{"wrapped proto exception", bg, fmt.Errorf("x: %w", &proto.Exception{Code: 60}), noAction, false},
		{"exception with eof cause", bg, errors.Join(&clickhouse.Exception{Code: 1}, io.EOF), noAction, false},
		{"dial error", bg, &net.OpError{Op: "dial", Err: timeoutNetErr{}}, noAction, false},
		{"wrapped dial error", bg, fmt.Errorf("x: %w", &net.OpError{Op: "dial", Err: syscall.ECONNRESET}), noAction, false},
		{"other", bg, errors.New("boom"), noAction, false},
		{"caller canceled", canceled, io.EOF, noAction, false},
		{"caller deadline, socket timeout", expired, timeoutErr, timedOut, false},
		{"caller deadline, ctx error", expired, context.DeadlineExceeded, timedOut, false},
		{"caller deadline, server exception", expired, &clickhouse.Exception{Code: 159}, noAction, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, retry := classify(tt.ctx, tt.err)
			assert.Equal(t, tt.want, v)
			assert.Equal(t, tt.retry, retry, "retry")
		})
	}

	t.Run("deadline passed while ctx.Err is still nil", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(bg, time.Now().Add(time.Hour))
		defer cancel()
		stale := &deadlineCtx{Context: ctx, deadline: time.Now().Add(-time.Millisecond)}
		require.NoError(t, stale.Err())
		v, retry := classify(stale, timeoutErr)
		assert.Equal(t, timedOut, v)
		assert.False(t, retry)
	})
}

type deadlineCtx struct {
	context.Context
	deadline time.Time
}

func (c *deadlineCtx) Deadline() (time.Time, bool) { return c.deadline, true }

func TestConnCallerDeadlineResetsWithoutRetry(t *testing.T) {
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	timeoutErr := &net.OpError{Op: "read", Err: timeoutNetErr{}}
	o := &fakeOpener{errs: []error{timeoutErr}}
	c := newTestConn(t, o, -1)

	assert.ErrorIs(t, c.Ping(expired), timeoutErr)
	assert.Equal(t, 2, o.count())
	assert.EqualValues(t, 1, o.get(0).calls.Load())
	assert.EqualValues(t, 0, o.get(1).calls.Load())
	assert.True(t, o.get(0).closed.Load())
}

func TestConnBatch(t *testing.T) {
	ctx := context.Background()

	t.Run("prepare conn error is retried once on the new pool", func(t *testing.T) {
		o := &fakeOpener{errs: []error{io.EOF}}
		c := newTestConn(t, o, -1)
		b, err := c.PrepareBatch(ctx, "insert")
		require.NoError(t, err)
		require.NoError(t, b.Close())
		assert.Equal(t, 2, o.count())
		assert.True(t, o.get(0).closed.Load())
	})

	t.Run("send conn error resets without retry", func(t *testing.T) {
		o := &fakeOpener{}
		c := newTestConn(t, o, -1)
		o.get(0).sendErr = io.EOF
		b, err := c.PrepareBatch(ctx, "insert")
		require.NoError(t, err)
		assert.ErrorIs(t, b.Send(), io.EOF)
		assert.Equal(t, 2, o.count())
		assert.True(t, o.get(0).closed.Load())
	})

	t.Run("server exception on send does not reset", func(t *testing.T) {
		o := &fakeOpener{}
		c := newTestConn(t, o, -1)
		o.get(0).sendErr = &clickhouse.Exception{Code: 60}
		b, err := c.PrepareBatch(ctx, "insert")
		require.NoError(t, err)
		require.Error(t, b.Send())
		assert.Equal(t, 1, o.count())
	})
}

func TestConnFormatCalls(t *testing.T) {
	ctx := context.Background()

	t.Run("query format retried once", func(t *testing.T) {
		o := &fakeOpener{errs: []error{io.EOF}}
		c := newTestConn(t, o, -1)
		r, err := c.QueryFormat(ctx, "CSV", "q")
		require.NoError(t, err)
		require.NoError(t, r.Close())
		assert.Equal(t, 2, o.count())
	})

	t.Run("insert format resets without retry", func(t *testing.T) {
		o := &fakeOpener{errs: []error{io.EOF}}
		c := newTestConn(t, o, -1)
		assert.ErrorIs(t, c.InsertFormat(ctx, "CSV", "q", strings.NewReader("x")), io.EOF)
		assert.Equal(t, 2, o.count())
		assert.EqualValues(t, 0, o.get(1).calls.Load())
	})
}

func TestConnRowsAndRowResetOnLaterErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("row scan", func(t *testing.T) {
		o := &fakeOpener{}
		c := newTestConn(t, o, -1)
		row := c.QueryRow(ctx, "q")
		require.NoError(t, row.Err())
		row.(*watchedRow).Row = fakeRow{err: io.EOF}
		assert.ErrorIs(t, row.Scan(), io.EOF)
		assert.Equal(t, 2, o.count())
	})

	t.Run("rows close", func(t *testing.T) {
		o := &fakeOpener{}
		c := newTestConn(t, o, -1)
		rows, err := c.Query(ctx, "q")
		require.NoError(t, err)
		rows.(*watchedRows).Rows = errRows{err: io.ErrUnexpectedEOF}
		assert.ErrorIs(t, rows.Close(), io.ErrUnexpectedEOF)
		assert.Equal(t, 2, o.count())
	})
}

type errRows struct {
	fakeRows
	err error
}

func (r errRows) Err() error { return r.err }

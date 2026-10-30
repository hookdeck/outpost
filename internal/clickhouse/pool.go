package clickhouse

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type opener func() (driver.Conn, error)

// pool is one driver connection pool. Once retired (replaced or closed for
// idleness) it closes when the last call using it returns. Connections held by
// Rows or Batches that are still open are closed by the driver on release.
type pool struct {
	conn    driver.Conn
	refs    int
	retired bool
	lastOK  time.Time
}

// conn wraps the driver connection pool: the pool is replaced when a call
// fails with a connection error (see isConnError) and closed after a period
// with no calls.
type conn struct {
	open    opener
	idle    time.Duration
	mu      sync.Mutex
	cur     *pool
	lastUse time.Time
	timer   *time.Timer
	closed  bool
}

var _ driver.Conn = (*conn)(nil)

func newConn(open opener, idle time.Duration) (*conn, error) {
	c := &conn{open: open, idle: idle}
	p, err := c.newPool()
	if err != nil {
		return nil, err
	}
	c.cur = p
	c.lastUse = time.Now()
	if idle > 0 {
		c.timer = time.AfterFunc(idle, c.idleCheck)
	}
	return c, nil
}

func (c *conn) newPool() (*pool, error) {
	d, err := c.open()
	if err != nil {
		return nil, err
	}
	return &pool{conn: d}, nil
}

// acquire returns the current pool with a reference held, opening one if the
// previous was closed for idleness.
func (c *conn) acquire() (*pool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, clickhouse.ErrConnectionClosed
	}
	if c.cur == nil {
		p, err := c.newPool()
		if err != nil {
			return nil, err
		}
		c.cur = p
		if c.timer != nil {
			c.timer.Reset(c.idle)
		}
	}
	c.lastUse = time.Now()
	c.cur.refs++
	return c.cur, nil
}

func (c *conn) release(p *pool) {
	c.mu.Lock()
	p.refs--
	c.lastUse = time.Now()
	shouldClose := p.retired && p.refs == 0
	c.mu.Unlock()
	if shouldClose {
		_ = p.conn.Close()
	}
}

// reset replaces p with a fresh pool unless it was already replaced.
func (c *conn) reset(p *pool) {
	c.mu.Lock()
	if c.closed || c.cur != p {
		c.mu.Unlock()
		return
	}
	next, err := c.newPool()
	if err != nil {
		c.mu.Unlock()
		return
	}
	c.cur = next
	p.retired = true
	closeNow := p.refs == 0
	c.mu.Unlock()
	if closeNow {
		_ = p.conn.Close()
	}
}

func (c *conn) idleCheck() {
	c.mu.Lock()
	if c.closed || c.cur == nil {
		c.mu.Unlock()
		return
	}
	if c.cur.refs > 0 {
		c.timer.Reset(c.idle)
		c.mu.Unlock()
		return
	}
	if remaining := c.idle - time.Since(c.lastUse); remaining > 0 {
		c.timer.Reset(remaining)
		c.mu.Unlock()
		return
	}
	p := c.cur
	c.cur = nil
	p.retired = true
	c.mu.Unlock()
	_ = p.conn.Close()
}

func (c *conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	if c.timer != nil {
		c.timer.Stop()
	}
	p := c.cur
	c.cur = nil
	c.mu.Unlock()
	if p == nil {
		return nil
	}
	return p.conn.Close()
}

type verdict int

const (
	noAction verdict = iota
	// connDropped: the connection is known unusable.
	connDropped
	// timedOut: the connection may be dead or merely slow.
	timedOut
)

// classify decides how to react to err from a call made with ctx. Caller
// cancels, server exceptions and dial failures need no reaction: a fresh pool
// would not help. A timeout is never retried: it can't tell a dead connection
// from a slow query.
func classify(ctx context.Context, err error) (v verdict, retry bool) {
	if err == nil || errors.Is(ctx.Err(), context.Canceled) {
		return noAction, false
	}
	var exc *clickhouse.Exception
	if errors.As(err, &exc) {
		return noAction, false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return noAction, false
	}
	callerDeadline := deadlineHit(ctx)
	switch {
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE):
		return connDropped, !callerDeadline
	case isTimeout(err):
		return timedOut, false
	}
	return noAction, false
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func deadlineHit(ctx context.Context) bool {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}
	d, ok := ctx.Deadline()
	return ok && !time.Now().Before(d)
}

func (c *conn) markOK(p *pool) {
	c.mu.Lock()
	p.lastOK = time.Now()
	c.mu.Unlock()
}

// react resets p after a failed call that started at start and reports
// whether the call may be retried. A timeout only resets the pool when no call
// on it succeeded since start: under load, slow calls time out on a healthy
// pool.
func (c *conn) react(ctx context.Context, p *pool, err error, start time.Time) (retry bool) {
	v, retry := classify(ctx, err)
	if v == noAction {
		return false
	}
	if v == timedOut {
		c.mu.Lock()
		healthy := p.lastOK.After(start)
		c.mu.Unlock()
		if healthy {
			return false
		}
	}
	c.reset(p)
	return retry
}

// do runs fn on the current pool. On a connection error the pool is replaced;
// a retryable call then runs once more on the new pool.
func (c *conn) do(ctx context.Context, retry bool, fn func(p *pool) error) error {
	start := time.Now()
	p, err := c.attempt(fn)
	if p == nil {
		return err
	}
	if !c.react(ctx, p, err, start) || !retry {
		return err
	}
	start = time.Now()
	p, err = c.attempt(fn)
	if p != nil {
		c.react(ctx, p, err, start)
	}
	return err
}

// attempt runs fn once; the returned pool is nil when none could be acquired.
func (c *conn) attempt(fn func(p *pool) error) (*pool, error) {
	p, err := c.acquire()
	if err != nil {
		return nil, err
	}
	defer c.release(p)
	err = fn(p)
	if err == nil {
		c.markOK(p)
	}
	return p, err
}

func (c *conn) Contributors() []string {
	p, err := c.acquire()
	if err != nil {
		return nil
	}
	defer c.release(p)
	return p.conn.Contributors()
}

func (c *conn) ServerVersion() (*driver.ServerVersion, error) {
	p, err := c.acquire()
	if err != nil {
		return nil, err
	}
	defer c.release(p)
	return p.conn.ServerVersion()
}

func (c *conn) Stats() driver.Stats {
	c.mu.Lock()
	p := c.cur
	c.mu.Unlock()
	if p == nil {
		return driver.Stats{}
	}
	return p.conn.Stats()
}

func (c *conn) Ping(ctx context.Context) error {
	return c.do(ctx, true, func(p *pool) error { return p.conn.Ping(ctx) })
}

func (c *conn) Select(ctx context.Context, dest any, query string, args ...any) error {
	return c.do(ctx, true, func(p *pool) error { return p.conn.Select(ctx, dest, query, args...) })
}

func (c *conn) Exec(ctx context.Context, query string, args ...any) error {
	return c.do(ctx, false, func(p *pool) error { return p.conn.Exec(ctx, query, args...) })
}

func (c *conn) AsyncInsert(ctx context.Context, query string, wait bool, args ...any) error {
	return c.do(ctx, false, func(p *pool) error { return p.conn.AsyncInsert(ctx, query, wait, args...) })
}

func (c *conn) InsertFormat(ctx context.Context, format string, query string, data io.Reader) error {
	return c.do(ctx, false, func(p *pool) error { return p.conn.InsertFormat(ctx, format, query, data) })
}

func (c *conn) QueryFormat(ctx context.Context, format string, query string, args ...any) (io.ReadCloser, error) {
	var r io.ReadCloser
	err := c.do(ctx, true, func(p *pool) (err error) {
		r, err = p.conn.QueryFormat(ctx, format, query, args...)
		return err
	})
	return r, err
}

func (c *conn) Query(ctx context.Context, query string, args ...any) (driver.Rows, error) {
	var rows driver.Rows
	var used *pool
	err := c.do(ctx, true, func(p *pool) (err error) {
		rows, err = p.conn.Query(ctx, query, args...)
		used = p
		return err
	})
	if err != nil {
		return nil, err
	}
	return &watchedRows{Rows: rows, w: watch{c, used, ctx, time.Now()}}, nil
}

func (c *conn) QueryRow(ctx context.Context, query string, args ...any) driver.Row {
	var row driver.Row
	var used *pool
	err := c.do(ctx, true, func(p *pool) error {
		row, used = p.conn.QueryRow(ctx, query, args...), p
		return row.Err()
	})
	if err != nil {
		return errRow{err}
	}
	return &watchedRow{Row: row, w: watch{c, used, ctx, time.Now()}}
}

func (c *conn) PrepareBatch(ctx context.Context, query string, opts ...driver.PrepareBatchOption) (driver.Batch, error) {
	var batch driver.Batch
	var used *pool
	err := c.do(ctx, true, func(p *pool) (err error) {
		batch, err = p.conn.PrepareBatch(ctx, query, opts...)
		used = p
		return err
	})
	if err != nil {
		return nil, err
	}
	return &watchedBatch{Batch: batch, w: watch{c, used, ctx, time.Now()}}, nil
}

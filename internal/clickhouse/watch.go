package clickhouse

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// watch replaces the pool a Rows, Row or Batch came from when its later
// reads or writes fail with a connection error.
type watch struct {
	c     *conn
	p     *pool
	ctx   context.Context
	start time.Time
}

func (w watch) check(err error) error {
	if err == nil {
		w.c.markOK(w.p)
		return nil
	}
	w.c.react(w.ctx, w.p, err, w.start)
	return err
}

type watchedRows struct {
	driver.Rows
	w watch
}

func (r *watchedRows) Close() error {
	err := r.Rows.Close()
	// Callers always Close but may skip Err, so Close checks the iteration error too.
	if err == nil {
		err = r.Rows.Err()
	}
	return r.w.check(err)
}

type watchedRow struct {
	driver.Row
	w watch
}

func (r *watchedRow) Scan(dest ...any) error { return r.w.check(r.Row.Scan(dest...)) }

func (r *watchedRow) ScanStruct(dest any) error { return r.w.check(r.Row.ScanStruct(dest)) }

type errRow struct{ err error }

func (r errRow) Err() error           { return r.err }
func (r errRow) Scan(...any) error    { return r.err }
func (r errRow) ScanStruct(any) error { return r.err }

type watchedBatch struct {
	driver.Batch
	w watch
}

func (b *watchedBatch) Send() error { return b.w.check(b.Batch.Send()) }

func (b *watchedBatch) Flush() error { return b.w.check(b.Batch.Flush()) }

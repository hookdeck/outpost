// Package sut runs the consumer under test in a worker process: Outpost's
// delivery queue consumer, built from Outpost configuration the way the
// service builds it, with a synthetic handler in place of the delivery logic.
// It reports what it does as a stream of events to the driver.
package sut

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Event kinds.
const (
	KindReady  = "ready"  // subscribed; T = time
	KindStart  = "start"  // handler started
	KindEnd    = "end"    // handler settled the message
	KindSample = "sample" // periodic process sample
	KindExit   = "exit"   // process about to exit
)

// Outcomes of a handler invocation.
const (
	OutcomeAck  = "ack"
	OutcomeNack = "nack"
)

// Event is one line of the worker's event stream (JSON).
type Event struct {
	K string `json:"k"`
	T int64  `json:"t"` // unix ns

	// start / end
	ID      string `json:"id,omitempty"`  // harness message id
	MID     string `json:"mid,omitempty"` // broker message id
	Attempt int    `json:"att,omitempty"` // broker delivery attempt, 0 = unknown
	Size    int    `json:"sz,omitempty"`  // body bytes
	Recv    int64  `json:"rt,omitempty"`  // when Receive returned it, unix ns; 0 = not seen
	Pub     int64  `json:"pt,omitempty"`  // publish time from the body, unix ns
	Outcome string `json:"o,omitempty"`   // end: ack | nack

	// sample
	Handled      int   `json:"h,omitempty"`   // handlers running now
	HandledMax   int   `json:"hm,omitempty"`  // max since the last sample
	HandledB     int64 `json:"hb,omitempty"`  // body bytes in running handlers
	HandledBMax  int64 `json:"hbm,omitempty"` // max since the last sample
	VisHeld      int   `json:"vh,omitempty"`  // returned by Receive, handler not started
	VisHeldMax   int   `json:"vhm,omitempty"`
	VisHeldB     int64 `json:"vhb,omitempty"`
	VisHeldBMax  int64 `json:"vhbm,omitempty"`
	RSS          int64 `json:"rss,omitempty"`  // bytes
	CPUNanos     int64 `json:"cpu,omitempty"`  // user + system, cumulative
	ErrorLogs    int64 `json:"errs,omitempty"` // error-level log lines so far
	WorkerFailed bool  `json:"failed,omitempty"`

	// exit
	Received  int      `json:"recv,omitempty"`
	Started   int      `json:"started,omitempty"`
	Unstarted []string `json:"unstarted,omitempty"` // harness ids received but never handled
	Err       string   `json:"err,omitempty"`
	PeakRSS   int64    `json:"peak_rss,omitempty"`
}

// Emitter writes events as JSON lines. Safe for concurrent use.
type Emitter struct {
	mu sync.Mutex
	w  *bufio.Writer
	c  io.Closer
}

// NewEmitter writes to w and flushes every 50 ms.
func NewEmitter(w io.WriteCloser) *Emitter {
	e := &Emitter{w: bufio.NewWriterSize(w, 1<<16), c: w}
	go func() {
		for range time.Tick(50 * time.Millisecond) {
			e.Flush()
		}
	}()
	return e
}

// Emit writes one event.
func (e *Emitter) Emit(ev Event) {
	b, _ := json.Marshal(ev)
	e.mu.Lock()
	e.w.Write(b)
	e.w.WriteByte('\n')
	e.mu.Unlock()
}

// Flush writes buffered events out.
func (e *Emitter) Flush() {
	e.mu.Lock()
	e.w.Flush()
	e.mu.Unlock()
}

// Close flushes and closes the stream.
func (e *Emitter) Close() {
	e.Flush()
	e.c.Close()
}

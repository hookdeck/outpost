package netguard

import (
	"container/list"
	"context"
	"sync"
	"sync/atomic"
)

// HostLimiter caps in-flight requests per callback host:port, so one slow
// receiver can't hold every worker (MCP_MAX_INFLIGHT_PER_HOST). TryAcquire
// never blocks; Acquire waits for a slot until its context is done, and
// waiters get freed slots in arrival order. Memory is bounded by the hosts
// with requests in flight or waiting, since an entry is deleted as soon as
// both drop to zero. Safe for concurrent use; a nil *HostLimiter, like
// maxPerHost <= 0, never limits.
type HostLimiter struct {
	max      int
	mu       sync.Mutex
	inflight map[string]*hostSlots
}

// hostSlots is one host's state. While waiters is non-empty every slot is
// held: a release hands its slot straight to the first waiter.
type hostSlots struct {
	n       int
	waiters list.List // of chan struct{}, each buffered 1
}

// NewHostLimiter allows up to maxPerHost concurrent requests per host:port.
func NewHostLimiter(maxPerHost int) *HostLimiter {
	return &HostLimiter{max: maxPerHost, inflight: make(map[string]*hostSlots)}
}

func noRelease() {}

// TryAcquire takes a slot for hostport, which should be the canonical
// (lowercase, explicit port) form so spellings of one host share a count.
// ok is false when the host is already at its limit. release is never nil;
// only its first call frees the slot, so a deferred release next to an
// explicit one can't free someone else's.
func (l *HostLimiter) TryAcquire(hostport string) (release func(), ok bool) {
	if l == nil || l.max <= 0 {
		return noRelease, true
	}
	l.mu.Lock()
	h := l.inflight[hostport]
	if h == nil {
		h = &hostSlots{}
		l.inflight[hostport] = h
	}
	if h.n >= l.max {
		l.mu.Unlock()
		return noRelease, false
	}
	h.n++
	l.mu.Unlock()
	return l.releaser(hostport), true
}

// Acquire takes a slot for hostport like TryAcquire, waiting for one until
// ctx is done; it then returns ctx's error and a no-op release. A done ctx
// never takes a slot.
func (l *HostLimiter) Acquire(ctx context.Context, hostport string) (release func(), err error) {
	if err := ctx.Err(); err != nil {
		return noRelease, err
	}
	if release, ok := l.TryAcquire(hostport); ok {
		return release, nil
	}

	l.mu.Lock()
	h := l.inflight[hostport]
	if h == nil || h.n < l.max {
		// Freed since TryAcquire.
		if h == nil {
			h = &hostSlots{}
			l.inflight[hostport] = h
		}
		h.n++
		l.mu.Unlock()
		return l.releaser(hostport), nil
	}
	handoff := make(chan struct{}, 1)
	elem := h.waiters.PushBack(handoff)
	l.mu.Unlock()

	select {
	case <-handoff:
		return l.releaser(hostport), nil
	case <-ctx.Done():
	}
	l.mu.Lock()
	select {
	case <-handoff:
		// A slot was handed over as ctx finished: pass it on.
		l.mu.Unlock()
		l.release(hostport)
	default:
		h.waiters.Remove(elem)
		l.mu.Unlock()
	}
	return noRelease, ctx.Err()
}

// releaser returns the release func of a slot taken for hostport.
func (l *HostLimiter) releaser(hostport string) func() {
	var released atomic.Bool
	return func() {
		if released.CompareAndSwap(false, true) {
			l.release(hostport)
		}
	}
}

// release frees a slot of hostport, handing it to the first waiter if any.
func (l *HostLimiter) release(hostport string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	h := l.inflight[hostport]
	if h == nil {
		return
	}
	if front := h.waiters.Front(); front != nil {
		h.waiters.Remove(front)
		front.Value.(chan struct{}) <- struct{}{}
		return
	}
	if h.n > 1 {
		h.n--
	} else {
		delete(l.inflight, hostport)
	}
}

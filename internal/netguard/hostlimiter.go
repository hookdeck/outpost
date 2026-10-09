package netguard

import (
	"sync"
	"sync/atomic"
)

// HostLimiter caps in-flight requests per callback host:port, so one slow
// receiver can't hold every worker (MCP_MAX_INFLIGHT_PER_HOST). Acquiring
// never blocks: a full host is the caller's cue to fail fast and retry
// later. Memory is bounded by the hosts with requests in flight, since an
// entry is deleted as soon as its count drops to zero. Safe for concurrent
// use; a nil *HostLimiter, like maxPerHost <= 0, never limits.
type HostLimiter struct {
	max      int
	mu       sync.Mutex
	inflight map[string]int
}

// NewHostLimiter allows up to maxPerHost concurrent requests per host:port.
func NewHostLimiter(maxPerHost int) *HostLimiter {
	return &HostLimiter{max: maxPerHost, inflight: make(map[string]int)}
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
	n := l.inflight[hostport]
	if n >= l.max {
		l.mu.Unlock()
		return noRelease, false
	}
	l.inflight[hostport] = n + 1
	l.mu.Unlock()

	var released atomic.Bool
	return func() {
		if released.CompareAndSwap(false, true) {
			l.release(hostport)
		}
	}, true
}

func (l *HostLimiter) release(hostport string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n := l.inflight[hostport]; n > 1 {
		l.inflight[hostport] = n - 1
	} else {
		delete(l.inflight, hostport)
	}
}

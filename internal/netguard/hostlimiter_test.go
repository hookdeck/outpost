package netguard_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostLimiter(t *testing.T) {
	t.Parallel()
	l := netguard.NewHostLimiter(2)

	r1, ok := l.TryAcquire("a.example:443")
	require.True(t, ok)
	r2, ok := l.TryAcquire("a.example:443")
	require.True(t, ok)
	full, ok := l.TryAcquire("a.example:443")
	assert.False(t, ok, "third request to a full host")
	require.NotNil(t, full)
	full() // a no-op, never frees someone else's slot
	_, ok = l.TryAcquire("a.example:443")
	assert.False(t, ok)

	// Other hosts have their own budget.
	rb, ok := l.TryAcquire("b.example:443")
	require.True(t, ok)

	r1()
	r1() // second call is a no-op
	r3, ok := l.TryAcquire("a.example:443")
	require.True(t, ok)
	_, ok = l.TryAcquire("a.example:443")
	assert.False(t, ok, "the double release freed only one slot")

	r2()
	r3()
	rb()
	assert.Zero(t, netguard.HostLimiterEntries(l), "idle hosts are forgotten")
}

func TestHostLimiter_Unlimited(t *testing.T) {
	t.Parallel()
	for _, l := range []*netguard.HostLimiter{nil, netguard.NewHostLimiter(0), netguard.NewHostLimiter(-1)} {
		for range 100 {
			release, ok := l.TryAcquire("a.example:443")
			require.True(t, ok)
			require.NotNil(t, release)
			defer release()
		}
		if l != nil {
			assert.Zero(t, netguard.HostLimiterEntries(l))
		}
	}
}

// Run with -race: many goroutines hammer a few hosts; no host ever exceeds
// its cap and the map is empty once everything is released.
func TestHostLimiter_Concurrent(t *testing.T) {
	t.Parallel()
	const (
		maxPerHost = 3
		hosts      = 4
		workers    = 32
		iterations = 500
	)
	l := netguard.NewHostLimiter(maxPerHost)
	var current [hosts]atomic.Int32
	var peak [hosts]atomic.Int32
	var acquired, refused atomic.Int64

	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range iterations {
				h := (w + i) % hosts
				release, ok := l.TryAcquire(fmt.Sprintf("h%d.example:443", h))
				if !ok {
					refused.Add(1)
					continue
				}
				acquired.Add(1)
				n := current[h].Add(1)
				for {
					p := peak[h].Load()
					if n <= p || peak[h].CompareAndSwap(p, n) {
						break
					}
				}
				current[h].Add(-1)
				release()
				release()
			}
		}()
	}
	wg.Wait()

	for h := range hosts {
		assert.LessOrEqual(t, peak[h].Load(), int32(maxPerHost), "host %d", h)
	}
	assert.Equal(t, int64(workers*iterations), acquired.Load()+refused.Load())
	assert.Positive(t, acquired.Load())
	assert.Zero(t, netguard.HostLimiterEntries(l))

	// Memory stays bounded by in-flight hosts, not hosts ever seen.
	for i := range 10000 {
		release, ok := l.TryAcquire(fmt.Sprintf("host-%d.example:443", i))
		require.True(t, ok)
		release()
	}
	assert.Zero(t, netguard.HostLimiterEntries(l))
}

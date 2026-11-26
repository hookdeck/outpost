package apirouter

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestPayloadListLimit_NeverExceedsTheCap drives the limiter from many
// goroutines at once and asserts the observed concurrency never exceeds the
// configured limit. -race needs cgo, which this box lacks, so this asserts the
// property directly instead of relying on the detector.
func TestPayloadListLimit_NeverExceedsTheCap(t *testing.T) {
	const limit = 3
	const workers = 24

	var current, peak int64
	var refused int64

	limiter := NewPayloadListLimiter(limit)
	limiter.retryAfter = 5 * time.Millisecond

	r := newPayloadListTestRouter(limiter, func(c *gin.Context) {
		// Only requests that actually got a slot reach the handler.
		n := atomic.AddInt64(&current, 1)
		for {
			p := atomic.LoadInt64(&peak)
			if n <= p || atomic.CompareAndSwapInt64(&peak, p, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		atomic.AddInt64(&current, -1)
		c.Status(http.StatusOK)
	})

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := doList(r, "?include=payload")
			if rec.Code == http.StatusServiceUnavailable {
				atomic.AddInt64(&refused, 1)
			} else {
				assert.Equal(t, http.StatusOK, rec.Code)
			}
		}()
	}
	wg.Wait()

	assert.LessOrEqual(t, atomic.LoadInt64(&peak), int64(limit),
		"observed concurrency must never exceed the configured limit")
	assert.Equal(t, int64(0), atomic.LoadInt64(&current), "every handler must release its slot")
	assert.Len(t, limiter.slots, 0, "no slot may leak after the burst")
	t.Logf("peak=%d refused=%d", peak, refused)
}

// TestPayloadListLimit_ConcurrentRefusalsAreConsistent exercises the waiting
// path under contention so a mis-synchronised channel use would surface as a
// panic, a negative count, or a leaked slot.
func TestPayloadListLimit_ConcurrentRefusalsAreConsistent(t *testing.T) {
	limiter := NewPayloadListLimiter(1)
	limiter.retryAfter = 1 * time.Millisecond

	r := newPayloadListTestRouter(limiter, func(c *gin.Context) {
		time.Sleep(3 * time.Millisecond)
		c.Status(http.StatusOK)
	})

	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := doList(r, "?include[]=payload")
			assert.Contains(t, []int{http.StatusOK, http.StatusServiceUnavailable}, rec.Code)
		}()
	}
	wg.Wait()

	assert.Len(t, limiter.slots, 0, "no slot may leak after contention")
}

package lru

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingEvict returns an eviction callback that parks on release for the
// given key (simulating a publisher Close waiting on an in-flight publish) and
// a channel that receives the key once the callback has started.
func blockingEvict(blockKey string, release <-chan struct{}) (func(string, int), <-chan string) {
	started := make(chan string, 16)
	return func(k string, _ int) {
		started <- k
		if k == blockKey {
			<-release
		}
	}, started
}

// withinDeadline fails the test when fn does not return within d.
func withinDeadline(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s blocked behind an eviction callback", what)
	}
}

func TestEvictCallbackRunsOutsideLock_SizeEviction(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	defer close(release)
	onEvict, started := blockingEvict("a", release)

	c := New[string, int](1, 0, onEvict)
	defer c.Close()
	c.Add("a", 1)

	// Adding "b" evicts "a"; its callback parks until release.
	evictorDone := make(chan bool, 1)
	go func() { evictorDone <- c.Add("b", 2) }()
	require.Equal(t, "a", <-started)

	withinDeadline(t, time.Second, "Get", func() {
		v, ok := c.Get("b")
		assert.True(t, ok)
		assert.Equal(t, 2, v)
	})
	withinDeadline(t, time.Second, "Add", func() {
		c.Add("b", 3) // update in place
	})
	withinDeadline(t, time.Second, "Len", func() {
		assert.Equal(t, 1, c.Len())
	})

	select {
	case <-evictorDone:
		t.Fatal("the evicting Add returned before its callback finished")
	default:
	}
	release <- struct{}{}
	assert.True(t, <-evictorDone, "Add reports the eviction")
}

func TestEvictCallbackRunsOutsideLock_ExpiredOnGet(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	defer close(release)
	onEvict, started := blockingEvict("a", release)

	// Stop the background cleanup loop up front (Close only stops it; TTLs
	// still apply) so the expiry is observed by Get.
	c := New[string, int](0, 50*time.Millisecond, onEvict)
	c.Close()
	c.Add("a", 1)
	time.Sleep(60 * time.Millisecond)

	getDone := make(chan bool, 1)
	go func() {
		_, ok := c.Get("a")
		getDone <- ok
	}()
	require.Equal(t, "a", <-started)

	withinDeadline(t, time.Second, "Add", func() { c.Add("b", 2) })
	withinDeadline(t, time.Second, "Get", func() {
		_, ok := c.Get("b")
		assert.True(t, ok)
	})

	release <- struct{}{}
	assert.False(t, <-getDone, "an expired entry is a miss")
}

func TestEvictCallbackRunsOutsideLock_CleanupLoop(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	onEvict, started := blockingEvict("a", release)

	c := New[string, int](0, 50*time.Millisecond, onEvict)
	defer c.Close()
	c.Add("a", 1)

	// The cleanup goroutine expires "a" and parks in its callback.
	select {
	case k := <-started:
		require.Equal(t, "a", k)
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup loop never evicted the expired entry")
	}

	withinDeadline(t, time.Second, "Add", func() { c.Add("b", 2) })
	withinDeadline(t, time.Second, "Get", func() {
		_, ok := c.Get("b")
		assert.True(t, ok)
	})
	close(release)
}

// A callback may call back into the cache: with callbacks under the lock this
// self-deadlocked.
func TestEvictCallbackMayReenterCache(t *testing.T) {
	t.Parallel()
	var c *Cache[string, int]
	var lenSeen atomic.Int64
	c = New[string, int](1, 0, func(string, int) {
		lenSeen.Store(int64(c.Len()))
	})
	defer c.Close()

	c.Add("a", 1)
	withinDeadline(t, time.Second, "re-entrant eviction", func() { c.Add("b", 2) })
	assert.Equal(t, int64(1), lenSeen.Load())
}

// Each evicted entry gets exactly one callback, with its own key and value.
func TestEvictCallbackOncePerEntry(t *testing.T) {
	t.Parallel()
	evicted := map[string]int{}
	c := New[string, int](2, 0, func(k string, v int) {
		evicted[k] += v
	})
	defer c.Close()

	c.Add("a", 1)
	c.Add("b", 2)
	c.Add("c", 3) // evicts a
	c.Add("d", 4) // evicts b
	assert.Equal(t, map[string]int{"a": 1, "b": 2}, evicted)
}

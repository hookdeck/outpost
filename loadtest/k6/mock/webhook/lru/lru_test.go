package lru

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBasicOperations(t *testing.T) {
	c := New[string, int](3, 0, nil)
	defer c.Close()

	// Test Add and Get
	c.Add("a", 1)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Errorf("Get(a) = (%v, %v), want (1, true)", v, ok)
	}

	// Test non-existent key
	if _, ok := c.Get("b"); ok {
		t.Error("Get(b) = true, want false")
	}

	// Test update existing key
	c.Add("a", 2)
	if v, _ := c.Get("a"); v != 2 {
		t.Errorf("Get(a) = %v, want 2", v)
	}

	// Test Len
	if l := c.Len(); l != 1 {
		t.Errorf("Len() = %v, want 1", l)
	}
}

func TestSizeEviction(t *testing.T) {
	var evicted []string
	onEvict := func(k string, v int) {
		evicted = append(evicted, k)
	}
	c := New[string, int](2, 0, onEvict)
	defer c.Close()

	// Add items up to capacity
	c.Add("a", 1)
	c.Add("b", 2)
	if c.Len() != 2 {
		t.Errorf("Len() = %v, want 2", c.Len())
	}

	// Add one more item, should evict oldest
	c.Add("c", 3)
	if c.Len() != 2 {
		t.Errorf("Len() = %v, want 2", c.Len())
	}
	if len(evicted) != 1 || evicted[0] != "a" {
		t.Errorf("evicted = %v, want [a]", evicted)
	}
	if _, ok := c.Get("a"); ok {
		t.Error("Get(a) = true, want false")
	}
}

func TestTTL(t *testing.T) {
	t.Run("basic expiration", func(t *testing.T) {
		var mu sync.Mutex
		var evicted []string
		onEvict := func(k string, v int) {
			mu.Lock()
			evicted = append(evicted, k)
			mu.Unlock()
		}
		c := New[string, int](0, 50*time.Millisecond, onEvict)
		defer c.Close()

		c.Add("a", 1)

		if v, ok := c.Get("a"); !ok || v != 1 {
			t.Error("Get(a) = false, want true")
		}

		time.Sleep(60 * time.Millisecond)

		if _, ok := c.Get("a"); ok {
			t.Error("Get(a) = true, want false (should be expired)")
		}

		time.Sleep(10 * time.Millisecond) // Give cleanup time to run
		mu.Lock()
		if len(evicted) != 1 || evicted[0] != "a" {
			t.Errorf("evicted = %v, want [a]", evicted)
		}
		mu.Unlock()
	})

	t.Run("refresh on access", func(t *testing.T) {
		var mu sync.Mutex
		var evicted []string
		onEvict := func(k string, v int) {
			mu.Lock()
			evicted = append(evicted, k)
			mu.Unlock()
		}
		c := New[string, int](0, 50*time.Millisecond, onEvict)
		defer c.Close()

		c.Add("a", 1)
		c.Add("b", 2)

		time.Sleep(30 * time.Millisecond)

		if v, ok := c.Get("a"); !ok || v != 1 {
			t.Error("Get(a) = false, want true after 30ms")
		}

		time.Sleep(30 * time.Millisecond)

		if _, ok := c.Get("b"); ok {
			t.Error("Get(b) = true, want false (should have expired)")
		}
		if v, ok := c.Get("a"); !ok || v != 1 {
			t.Error("Get(a) = false, want true (should be refreshed)")
		}

		time.Sleep(10 * time.Millisecond) // Give cleanup time to run
		mu.Lock()
		if len(evicted) != 1 || evicted[0] != "b" {
			t.Errorf("evicted = %v, want [b]", evicted)
		}
		mu.Unlock()

		time.Sleep(60 * time.Millisecond)
		if _, ok := c.Get("a"); ok {
			t.Error("Get(a) = true, want false (should have expired)")
		}

		time.Sleep(10 * time.Millisecond) // Give cleanup time to run
		mu.Lock()
		if len(evicted) != 2 || evicted[1] != "a" {
			t.Errorf("evicted = %v, want [b, a]", evicted)
		}
		mu.Unlock()
	})
}

func TestConcurrentAccess(t *testing.T) {
	c := New[int, int](1000, time.Second, nil)
	defer c.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				k := base*100 + j
				c.Add(k, k)
				if v, ok := c.Get(k); !ok || v != k {
					t.Errorf("Get(%v) = (%v, %v), want (%v, true)", k, v, ok, k)
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestCleanupBehavior(t *testing.T) {
	ttl := 100 * time.Millisecond
	var evicted []string
	c := New[string, int](0, ttl, func(k string, v int) {
		evicted = append(evicted, k)
	})
	defer c.Close()

	// Add items with staggered times
	c.Add("a", 1)
	time.Sleep(40 * time.Millisecond)
	c.Add("b", 2)
	time.Sleep(40 * time.Millisecond)
	c.Add("c", 3)

	// Wait for cleanup to run
	time.Sleep(ttl)

	// All items should be evicted by cleanup goroutine
	// without explicitly calling Get
	time.Sleep(20 * time.Millisecond) // Give cleanup goroutine time to work

	if c.Len() != 0 {
		t.Errorf("Len() = %v, want 0 (all items should be cleaned up)", c.Len())
	}

	// Verify eviction order
	if len(evicted) != 3 {
		t.Errorf("got %v evictions, want 3", len(evicted))
	}
	// a should be evicted first, then b, then c
	for i, want := range []string{"a", "b", "c"} {
		if i >= len(evicted) || evicted[i] != want {
			t.Errorf("evicted[%d] = %v, want %v", i, evicted[i], want)
		}
	}
}

func TestEvictionCallback(t *testing.T) {
	t.Run("size-based eviction", func(t *testing.T) {
		var mu sync.Mutex
		var evicted []string
		onEvict := func(k string, v int) {
			mu.Lock()
			evicted = append(evicted, k)
			mu.Unlock()
		}
		c := New[string, int](2, 0, onEvict)
		defer c.Close()

		c.Add("a", 1)
		c.Add("b", 2)

		mu.Lock()
		assert.Empty(t, evicted, "No eviction should occur when under capacity")
		mu.Unlock()

		c.Add("c", 3)

		time.Sleep(10 * time.Millisecond) // Give eviction time to complete
		mu.Lock()
		assert.Equal(t, []string{"a"}, evicted, "Should evict oldest item")
		mu.Unlock()

		c.Add("b", 20)

		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		assert.Equal(t, []string{"a"}, evicted, "Update should not trigger eviction")
		mu.Unlock()
	})

	t.Run("TTL-based eviction", func(t *testing.T) {
		var mu sync.Mutex
		var evicted []string
		onEvict := func(k string, v int) {
			mu.Lock()
			evicted = append(evicted, k)
			mu.Unlock()
		}
		c := New[string, int](0, 50*time.Millisecond, onEvict)
		defer c.Close()

		c.Add("a", 1)
		c.Add("b", 2)

		mu.Lock()
		assert.Empty(t, evicted, "No eviction should occur before TTL")
		mu.Unlock()

		time.Sleep(60 * time.Millisecond)
		c.Get("a") // Trigger cleanup

		time.Sleep(10 * time.Millisecond) // Give cleanup time to run
		mu.Lock()
		assert.ElementsMatch(t, []string{"a", "b"}, evicted, "Both items should be evicted after TTL")
		mu.Unlock()
	})

	t.Run("cleanup goroutine eviction", func(t *testing.T) {
		var mu sync.Mutex
		var evicted []string
		onEvict := func(k string, v int) {
			mu.Lock()
			evicted = append(evicted, k)
			mu.Unlock()
		}
		c := New[string, int](0, 100*time.Millisecond, onEvict)
		defer c.Close()

		c.Add("a", 1)
		time.Sleep(40 * time.Millisecond)
		c.Add("b", 2)
		time.Sleep(40 * time.Millisecond)
		c.Add("c", 3)

		// Wait for first batch to expire and be cleaned up
		time.Sleep(100 * time.Millisecond)

		// Check eviction (with retry since cleanup is async)
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			mu.Lock()
			evictedCopy := make([]string, len(evicted))
			copy(evictedCopy, evicted)
			mu.Unlock()

			if len(evictedCopy) >= 2 {
				assert.Subset(t, evictedCopy, []string{"a", "b"},
					"First two items should be evicted")
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Wait for last item to expire
		time.Sleep(100 * time.Millisecond)

		// Check final eviction (with retry)
		deadline = time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			mu.Lock()
			evictedCopy := make([]string, len(evicted))
			copy(evictedCopy, evicted)
			mu.Unlock()

			if len(evictedCopy) == 3 {
				assert.ElementsMatch(t, []string{"a", "b", "c"}, evictedCopy,
					"All items should eventually be evicted")
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	})

	t.Run("close with items", func(t *testing.T) {
		var evicted []string
		onEvict := func(k string, v int) {
			evicted = append(evicted, k)
		}
		c := New[string, int](0, time.Hour, onEvict)

		// Add items
		c.Add("a", 1)
		c.Add("b", 2)
		assert.Empty(t, evicted, "No eviction should occur")

		// Close cache
		c.Close()
		assert.Empty(t, evicted,
			"Close should not trigger eviction callbacks")
	})

	t.Run("nil callback", func(t *testing.T) {
		c := New[string, int](1, time.Hour, nil)
		defer c.Close()

		c.Add("a", 1)
		c.Add("b", 2) // Should evict a without panic
	})
}

func TestEdgeCases(t *testing.T) {
	// Test zero/negative size
	c := New[string, int](-1, 0, nil)
	defer c.Close()
	if c.size != 0 {
		t.Errorf("size = %v, want 0 for negative input", c.size)
	}

	// Test zero/negative TTL
	c = New[string, int](0, -1, nil)
	defer c.Close()
	if c.ttl != 0 {
		t.Errorf("ttl = %v, want 0 for negative input", c.ttl)
	}

	// Test nil callback
	c = New[string, int](1, 0, nil)
	defer c.Close()
	c.Add("a", 1)
	c.Add("b", 2) // Should not panic with nil callback

	// Test empty cache operations
	if _, ok := c.Get("nonexistent"); ok {
		t.Error("Get on empty cache returned true")
	}
	if l := c.Len(); l != 1 {
		t.Errorf("Len() = %v, want 1", l)
	}
}

func TestLRUBehavior(t *testing.T) {
	c := New[string, int](2, 0, nil)
	defer c.Close()

	c.Add("a", 1)
	c.Add("b", 2)

	// Access "a" to make it most recently used
	c.Get("a")

	// Add new item, should evict "b" instead of "a"
	c.Add("c", 3)

	if _, ok := c.Get("b"); ok {
		t.Error("b should have been evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Error("a should still be present")
	}
	if _, ok := c.Get("c"); !ok {
		t.Error("c should be present")
	}
}

func BenchmarkLRU_Add(b *testing.B) {
	l := New[int, int](8192, 0, nil)
	defer l.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Add(i, i)
	}
}

func BenchmarkLRU_Get(b *testing.B) {
	l := New[int, int](8192, 0, nil)
	defer l.Close()

	for i := 0; i < 8192; i++ {
		l.Add(i, i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Get(i % 8192)
	}
}

func BenchmarkLRU_GetMiss(b *testing.B) {
	l := New[int, int](8192, 0, nil)
	defer l.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Get(i)
	}
}

func BenchmarkLRU_Mixed(b *testing.B) {
	l := New[int, int](8192, 0, nil)
	defer l.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			l.Add(i, i)
		} else {
			l.Get(i / 2)
		}
	}
}

func BenchmarkLRU_WithTTL(b *testing.B) {
	l := New[int, int](8192, time.Hour, nil)
	defer l.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			l.Add(i, i)
		} else {
			l.Get(i / 2)
		}
	}
}

func BenchmarkLRU_WithEvict(b *testing.B) {
	onEvict := func(k, v int) {}
	l := New[int, int](8192, 0, onEvict)
	defer l.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Add(i, i)
	}
}

func BenchmarkLRU_WithEvictWork(b *testing.B) {
	sum := 0
	onEvict := func(k, v int) {
		sum += v
	}
	l := New[int, int](8192, 0, onEvict)
	defer l.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Add(i, i)
	}
}

func BenchmarkLRU_HighContention(b *testing.B) {
	l := New[int, int](8192, 0, nil)
	defer l.Close()

	// Pre-populate
	for i := 0; i < 8192; i++ {
		l.Add(i, i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		// Use a local counter to avoid shared state
		counter := 0
		for pb.Next() {
			counter++
			if counter%2 == 0 {
				l.Add(counter%8192, counter)
			} else {
				l.Get(counter % 8192)
			}
		}
	})
}

func BenchmarkLRU_Rand_NoExpire(b *testing.B) {
	l := New[int64, int64](8192, 0, nil)
	defer l.Close()

	trace := make([]int64, b.N*2)
	for i := 0; i < b.N*2; i++ {
		trace[i] = rand.Int63() % 32768
	}

	b.ResetTimer()

	var hit, miss int
	for i := 0; i < 2*b.N; i++ {
		if i%2 == 0 {
			l.Add(trace[i], trace[i])
		} else {
			if _, ok := l.Get(trace[i]); ok {
				hit++
			} else {
				miss++
			}
		}
	}
	b.Logf("hit: %d miss: %d ratio: %f", hit, miss, float64(hit)/float64(hit+miss))
}

func BenchmarkLRU_Freq_NoExpire(b *testing.B) {
	l := New[int64, int64](8192, 0, nil)
	defer l.Close()

	trace := make([]int64, b.N*2)
	for i := 0; i < b.N*2; i++ {
		if i%2 == 0 {
			trace[i] = rand.Int63() % 16384
		} else {
			trace[i] = rand.Int63() % 32768
		}
	}

	b.ResetTimer()

	var hit, miss int
	for i := 0; i < 2*b.N; i++ {
		if i%2 == 0 {
			l.Add(trace[i], trace[i])
		} else {
			if _, ok := l.Get(trace[i]); ok {
				hit++
			} else {
				miss++
			}
		}
	}
	b.Logf("hit: %d miss: %d ratio: %f", hit, miss, float64(hit)/float64(hit+miss))
}

func BenchmarkLRU_Rand_WithExpire(b *testing.B) {
	l := New[int64, int64](8192, time.Second, nil)
	defer l.Close()

	trace := make([]int64, b.N*2)
	for i := 0; i < b.N*2; i++ {
		trace[i] = rand.Int63() % 32768
	}

	b.ResetTimer()

	var hit, miss int
	for i := 0; i < 2*b.N; i++ {
		if i%2 == 0 {
			l.Add(trace[i], trace[i])
		} else {
			if _, ok := l.Get(trace[i]); ok {
				hit++
			} else {
				miss++
			}
		}
	}
	b.Logf("hit: %d miss: %d ratio: %f", hit, miss, float64(hit)/float64(hit+miss))
}

func BenchmarkLRU_Freq_WithExpire(b *testing.B) {
	l := New[int64, int64](8192, time.Second, nil)
	defer l.Close()

	trace := make([]int64, b.N*2)
	for i := 0; i < b.N*2; i++ {
		if i%2 == 0 {
			trace[i] = rand.Int63() % 16384
		} else {
			trace[i] = rand.Int63() % 32768
		}
	}

	b.ResetTimer()

	var hit, miss int
	for i := 0; i < 2*b.N; i++ {
		if i%2 == 0 {
			l.Add(trace[i], trace[i])
		} else {
			if _, ok := l.Get(trace[i]); ok {
				hit++
			} else {
				miss++
			}
		}
	}
	b.Logf("hit: %d miss: %d ratio: %f", hit, miss, float64(hit)/float64(hit+miss))
}

// Package lru implements an LRU cache with TTL that refreshes on access
package lru

import (
	"sync"
	"time"
)

// EvictCallback is used to get a callback when a cache entry is evicted
type EvictCallback[K comparable, V any] func(key K, value V)

type entry[K comparable, V any] struct {
	key        K
	value      V
	expiresAt  time.Time
	bucket     uint8
	prev, next *entry[K, V]
}

const numBuckets = 100

type bucket[K comparable, V any] struct {
	entries     map[K]*entry[K, V]
	newestEntry time.Time
}

// Cache implements a thread-safe LRU cache with TTL that refreshes on access
type Cache[K comparable, V any] struct {
	size       int
	ttl        time.Duration
	items      map[K]*entry[K, V]
	onEvict    func(key K, value V)
	head       *entry[K, V]
	tail       *entry[K, V]
	mu         sync.Mutex
	done       chan struct{}
	buckets    []bucket[K, V]
	nextBucket uint8
}

// New creates a new Cache with the given size limit, TTL, and optional eviction callback.
// Size of 0 means no limit. TTL of 0 means no expiration.
func New[K comparable, V any](size int, ttl time.Duration, onEvict func(key K, value V)) *Cache[K, V] {
	if size < 0 {
		size = 0
	}
	if ttl <= 0 {
		ttl = 0 // No expiration
	}

	c := &Cache[K, V]{
		size:    size,
		ttl:     ttl,
		items:   make(map[K]*entry[K, V]),
		onEvict: onEvict,
		done:    make(chan struct{}),
	}

	// Initialize buckets
	if ttl > 0 {
		c.buckets = make([]bucket[K, V], numBuckets)
		for i := 0; i < numBuckets; i++ {
			c.buckets[i] = bucket[K, V]{entries: make(map[K]*entry[K, V])}
		}

		// Start cleanup goroutine
		go func() {
			ticker := time.NewTicker(ttl / numBuckets)
			defer ticker.Stop()
			for {
				select {
				case <-c.done:
					return
				case <-ticker.C:
					c.deleteExpired()
				}
			}
		}()
	}

	return c
}

func (c *Cache[K, V]) deleteExpired() {
	c.mu.Lock()
	bucketIdx := c.nextBucket
	now := time.Now()

	// Check all items in current bucket
	for key, e := range c.buckets[bucketIdx].entries {
		if now.After(e.expiresAt) {
			c.remove(e)
		} else {
			// Move to next bucket if not expired
			delete(c.buckets[bucketIdx].entries, key)
			nextBucket := (bucketIdx + 1) % numBuckets
			e.bucket = nextBucket
			c.buckets[nextBucket].entries[key] = e
			if c.buckets[nextBucket].newestEntry.Before(e.expiresAt) {
				c.buckets[nextBucket].newestEntry = e.expiresAt
			}
		}
	}

	// Also check items in the items map that might have expired
	for _, e := range c.items {
		if now.After(e.expiresAt) {
			c.remove(e)
		}
	}

	c.nextBucket = (c.nextBucket + 1) % numBuckets
	c.mu.Unlock()
}

func (c *Cache[K, V]) addToBucket(e *entry[K, V]) {
	bucketID := (numBuckets + c.nextBucket - 1) % numBuckets
	e.bucket = bucketID
	c.buckets[bucketID].entries[e.key] = e
	if c.buckets[bucketID].newestEntry.Before(e.expiresAt) {
		c.buckets[bucketID].newestEntry = e.expiresAt
	}
}

func (c *Cache[K, V]) removeFromBucket(e *entry[K, V]) {
	delete(c.buckets[e.bucket].entries, e.key)
}

// Add adds a value to the cache, returns true if an eviction occurred
func (c *Cache[K, V]) Add(key K, value V) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if e, ok := c.items[key]; ok {
		c.moveToFront(e)
		if c.ttl > 0 {
			c.removeFromBucket(e)
			e.expiresAt = now.Add(c.ttl)
			c.addToBucket(e)
		}
		e.value = value
		return false
	}

	e := &entry[K, V]{
		key:   key,
		value: value,
	}
	if c.ttl > 0 {
		e.expiresAt = now.Add(c.ttl)
	}

	c.items[key] = e
	c.addToFront(e)
	if c.ttl > 0 {
		c.addToBucket(e)
	}

	evicted := false
	if c.size > 0 && len(c.items) > c.size {
		c.removeLRU()
		evicted = true
	}

	return evicted
}

// Get looks up a key's value from the cache, refreshing TTL if found
func (c *Cache[K, V]) Get(key K) (value V, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.items[key]; ok {
		if c.ttl > 0 {
			now := time.Now()
			if now.After(e.expiresAt) {
				c.remove(e)
				return value, false
			}
			c.removeFromBucket(e)
			e.expiresAt = now.Add(c.ttl)
			c.addToBucket(e)
		}
		c.moveToFront(e)
		return e.value, true
	}
	return value, false
}

func (c *Cache[K, V]) moveToFront(e *entry[K, V]) {
	if e == c.head {
		return
	}
	c.removeFromList(e)
	c.addToFront(e)
}

func (c *Cache[K, V]) addToFront(e *entry[K, V]) {
	if c.head == nil {
		c.head = e
		c.tail = e
		return
	}
	e.next = c.head
	c.head.prev = e
	c.head = e
}

func (c *Cache[K, V]) removeFromList(e *entry[K, V]) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev = nil
	e.next = nil
}

func (c *Cache[K, V]) remove(e *entry[K, V]) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	if c.ttl > 0 {
		c.removeFromBucket(e)
	}
	delete(c.items, e.key)
	if c.onEvict != nil {
		c.onEvict(e.key, e.value)
	}
}

func (c *Cache[K, V]) removeLRU() {
	if c.tail != nil {
		c.remove(c.tail)
	}
}

// Len returns the number of items in the cache
func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Close stops the cleanup goroutine
func (c *Cache[K, V]) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ttl > 0 {
		select {
		case <-c.done:
			return
		default:
		}
		close(c.done)
	}
}

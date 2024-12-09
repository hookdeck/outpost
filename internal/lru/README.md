# LRU Cache with TTL Refresh

A thread-safe LRU cache implementation with TTL that refreshes on access. This implementation differs from hashicorp's expirable LRU primarily in its TTL behavior.

## Key Features

- Generic type parameters for key and value
- Thread-safe operations
- Size-based eviction (LRU)
- TTL with refresh on access
- Optional eviction callbacks
- Efficient memory usage

## Comparison with hashicorp/golang-lru/v2/expirable

The main difference is in TTL behavior:

- **Our Implementation**: Refreshes TTL on every Get access, keeping frequently accessed items alive
- **Hashicorp's Implementation**: TTL is set only once when item is added, items expire at fixed time regardless of usage

This makes our implementation better suited for caching frequently accessed data where you want to keep "hot" items in cache, while hashicorp's is better for strict time-based expiration.

## Performance Comparison

Benchmark results (M1 Pro, Go 1.21):

```
Operation          | Our Implementation | Hashicorp's Implementation
-------------------|-------------------|------------------------
Rand_NoExpire      | 161.5 ns/op      | 279.1 ns/op
Rand_WithExpire    | 284.4 ns/op      | 307.4 ns/op
Freq_NoExpire      | 145.4 ns/op      | 268.7 ns/op
Freq_WithExpire    | 259.9 ns/op      | 261.2 ns/op
```

Additional performance metrics for our implementation:
```
Operation          | Time      | Memory Usage
-------------------|-----------|-------------
Add                | 179.7 ns  | 64 B/op
Get (hit)          | 28.19 ns  | 0 B/op
Get (miss)         | 13.67 ns  | 0 B/op
Mixed Ops          | 84.84 ns  | 32 B/op
High Contention    | 159.2 ns  | 0 B/op
```

Key performance characteristics:
- Zero allocation for Get operations
- Low memory overhead (32-64 bytes per entry)
- Excellent concurrent performance under high contention
- Fast TTL refresh (~131ns including TTL update)

## Usage

```go
// Create cache with size limit of 100, TTL of 1 hour
cache := New[string, int](100, time.Hour, nil)
defer cache.Close()

// Add items
cache.Add("key", 123)

// Get refreshes TTL
if val, ok := cache.Get("key"); ok {
    // TTL is refreshed, item will live for another hour
}

// Optional eviction callback
onEvict := func(key string, value int) {
    fmt.Printf("Evicted: %v\n", key)
}
cache := New[string, int](100, time.Hour, onEvict)
```

## Implementation Details

1. **Cleanup Strategy**:
   - Runs cleanup every TTL/100 period
   - Uses bucketed expiration for efficient cleanup
   - Maximum overstay is TTL * 1.01

2. **Thread Safety**:
   - Uses a single mutex for all operations
   - Optimized for high concurrency (159.2 ns/op under contention)
   - Zero allocations for read operations

3. **Memory Efficiency**:
   - Single allocation per entry
   - Direct pointer manipulation without extra indirection
   - Zero allocations for Get operations
   - Bucketed expiration reduces cleanup overhead

4. **TTL Refresh**:
   - Constant time TTL refresh on Get
   - Efficient bucket management for expiration
   - No extra allocations for TTL updates
</rewritten_file> 
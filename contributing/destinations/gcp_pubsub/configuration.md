# GCP Pub/Sub Destination Configuration

This document outlines implementation decisions and potential configuration options for the GCP Pub/Sub destination.

## Current Implementation Decisions

### Message Structure
- **Body**: Event data is JSON-marshaled
- **Attributes**: Outpost metadata is passed as Pub/Sub attributes (string key-value pairs)

### Configuration
Currently minimal - only supports:
```golang
type Config struct {
    ProjectID string
    Topic     string
    Endpoint  string // For emulator support
}
```

### Authentication
- Only supports service account JSON credentials
- No support for Application Default Credentials (ADC)
- No support for Workload Identity

### Publishing Behavior
- **Synchronous**: Each publish waits for acknowledgment (no fire-and-forget)
- **No batching**: Each event published individually
- **Topic validation**: Commented out for performance (let it fail on publish instead)

## Potential Configuration Options

### Message Configuration
GCP Pub/Sub supports these message fields we're NOT using:

```golang
type Message struct {
    OrderingKey string    // For message ordering within same key
    // Attributes map[string]string - We use this for metadata
}
```

**Ordering Key**: Could add `ordering_key_template` config similar to Kinesis partition key:
- Enable FIFO delivery for messages with same ordering key
- Requires enabling message ordering on the topic
- Trade-off: Can reduce throughput

### Publisher Configuration
The Go SDK supports these publisher settings we could expose:

```golang
topic.PublishSettings = pubsub.PublishSettings{
    // Batching
    DelayThreshold: 100 * time.Millisecond,  // Max time to wait before sending batch
    CountThreshold: 100,                      // Max messages in batch
    ByteThreshold:  1e6,                      // Max bytes in batch
    
    // Concurrency
    NumGoroutines: 10,                        // Parallel publish goroutines
    
    // Flow control
    FlowControlSettings: pubsub.FlowControlSettings{
        MaxOutstandingMessages: 1000,
        MaxOutstandingBytes:    1e9,
        LimitExceededBehavior:  pubsub.FlowControlBlock, // or FlowControlIgnore
    },
    
    // Timeout
    Timeout: 60 * time.Second,
}
```

Potential configs:
- `enable_batching`: Toggle batch publishing
- `batch_size`: Max messages per batch
- `batch_delay_ms`: Max delay before sending partial batch
- `timeout_seconds`: Publish timeout
- `max_concurrent_publishes`: Parallelism limit

### Topic Configuration
- `enable_message_ordering`: Requires ordering_key support
- `retention_duration`: How long Pub/Sub retains unacknowledged messages
- `message_storage_policy`: Region restrictions for data residency

### Connection Options
- `enable_compression`: gRPC compression
- `keepalive_time_seconds`: Connection keepalive interval
- `max_connection_idle_seconds`: When to close idle connections

## Design Questions

1. **Should we support ordering keys?**
   - Pro: Enables FIFO delivery for related events
   - Con: Reduces throughput, requires topic configuration

2. **Should we enable batching by default?**
   - Pro: Better throughput, lower costs
   - Con: Adds latency, complicates error handling

3. **Should we validate topic existence?**
   - Currently disabled for performance
   - Could make it configurable: `skip_topic_validation`

4. **Should we support Application Default Credentials?**
   - Would work better in GKE/Cloud Run environments
   - Less configuration needed for GCP-hosted Outpost

5. **Should we expose publisher timeout?**
   - Current: Uses default (60s)
   - Could add `publish_timeout_seconds`

## Not Implemented (Intentionally)

1. **Push subscriptions**: Outpost is a publisher, not a subscriber
2. **Topic management**: No auto-creation of topics
3. **Schema validation**: Could support Pub/Sub schemas in future
4. **Dead letter topics**: Publisher-side concern only
5. **Encryption keys**: Uses Google-managed encryption
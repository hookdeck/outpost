# GetAttemptMetricsMeasuresUnion

Measures to compute. At least one required. Rate measures (`rate`, `successful_rate`, `failed_rate`) are throughput in events/second. Latency measures (`avg_latency`, `p50_latency`, `p95_latency`, `p99_latency`) are destination response times in milliseconds, `null` when no attempt in the bucket recorded a latency. Use bracket notation for multiple values (e.g., `measures[0]=count&measures[1]=error_rate`).


## Supported Types

### `operations.GetAttemptMetricsMeasuresEnum1`

```typescript
const value: operations.GetAttemptMetricsMeasuresEnum1 = "p99_latency";
```

### `operations.GetAttemptMetricsMeasuresEnum2[]`

```typescript
const value: operations.GetAttemptMetricsMeasuresEnum2[] = [];
```


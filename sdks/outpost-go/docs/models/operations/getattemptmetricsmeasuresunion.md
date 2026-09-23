# GetAttemptMetricsMeasuresUnion

Measures to compute. At least one required. Rate measures (`rate`, `successful_rate`, `failed_rate`) are throughput in events/second. Latency measures (`avg_latency`, `p50_latency`, `p95_latency`, `p99_latency`) are destination response times in milliseconds, `null` when no attempt in the bucket recorded a latency. Use bracket notation for multiple values (e.g., `measures[0]=count&measures[1]=error_rate`).


## Supported Types

### GetAttemptMetricsMeasuresEnum1

```go
getAttemptMetricsMeasuresUnion := operations.CreateGetAttemptMetricsMeasuresUnionGetAttemptMetricsMeasuresEnum1(operations.GetAttemptMetricsMeasuresEnum1{/* values here */})
```

### 

```go
getAttemptMetricsMeasuresUnion := operations.CreateGetAttemptMetricsMeasuresUnionArrayOfGetAttemptMetricsMeasuresEnum2([]operations.GetAttemptMetricsMeasuresEnum2{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch getAttemptMetricsMeasuresUnion.Type {
	case operations.GetAttemptMetricsMeasuresUnionTypeGetAttemptMetricsMeasuresEnum1:
		// getAttemptMetricsMeasuresUnion.GetAttemptMetricsMeasuresEnum1 is populated
	case operations.GetAttemptMetricsMeasuresUnionTypeArrayOfGetAttemptMetricsMeasuresEnum2:
		// getAttemptMetricsMeasuresUnion.ArrayOfGetAttemptMetricsMeasuresEnum2 is populated
}
```

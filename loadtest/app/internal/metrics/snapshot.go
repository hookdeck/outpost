package metrics

// GroupSnapshot is the JSON-serializable metrics for a single group.
type GroupSnapshot struct {
	PublishTotal       int64   `json:"publish_total"`
	PublishErrors      int64   `json:"publish_errors"`
	DeliveryTotal      int64   `json:"delivery_total"`
	Duplicates         int64   `json:"duplicates"`
	MissingTotal       int64   `json:"missing_total"`
	CutoffTotal        int64   `json:"cutoff_total"`
	PublishRatePerSec  float64 `json:"publish_rate_per_sec"`
	DeliveryRatePerSec float64 `json:"delivery_rate_per_sec"`
	PublishLatencyP50  int64   `json:"publish_latency_p50_ms"`
	PublishLatencyP90  int64   `json:"publish_latency_p90_ms"`
	PublishLatencyP95  int64   `json:"publish_latency_p95_ms"`
	PublishLatencyP99  int64   `json:"publish_latency_p99_ms"`
	PublishLatencyMax  int64   `json:"publish_latency_max_ms"`
	E2ELatencyP50      int64   `json:"e2e_latency_p50_ms"`
	E2ELatencyP90      int64   `json:"e2e_latency_p90_ms"`
	E2ELatencyP95      int64   `json:"e2e_latency_p95_ms"`
	E2ELatencyP99      int64   `json:"e2e_latency_p99_ms"`
	E2ELatencyMax      int64   `json:"e2e_latency_max_ms"`
	GeneratorLagP50    int64   `json:"generator_lag_p50_ms"`
	GeneratorLagP99    int64   `json:"generator_lag_p99_ms"`
	InFlight           int     `json:"in_flight"`
}

// AggregateSnapshot is the JSON-serializable aggregate across all groups.
type AggregateSnapshot struct {
	Groups map[string]GroupSnapshot `json:"groups"`
	Total  GroupSnapshot            `json:"total"`
}

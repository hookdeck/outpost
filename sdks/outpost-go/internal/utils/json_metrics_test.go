package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Minimal reproduction of MetricsDataPoint matching the generated SDK types.
// time_bucket and granularity are now *time.Time / *string (non-nullable in spec).
type testMetricsDataPoint struct {
	TimeBucket *time.Time        `json:"time_bucket,omitempty"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
	Metrics    map[string]any    `json:"metrics,omitempty"`
}

func (m testMetricsDataPoint) MarshalJSON() ([]byte, error) {
	return MarshalJSON(m, "", false)
}

func (m *testMetricsDataPoint) UnmarshalJSON(data []byte) error {
	return UnmarshalJSON(data, &m, "", false, nil)
}

type testMetricsMetadata struct {
	Granularity *string `json:"granularity,omitempty"`
	QueryTimeMs *int64  `json:"query_time_ms,omitempty"`
	RowCount    *int64  `json:"row_count,omitempty"`
	RowLimit    *int64  `json:"row_limit,omitempty"`
	Truncated   *bool   `json:"truncated,omitempty"`
}

type testMetricsResponse struct {
	Data     []testMetricsDataPoint `json:"data,omitempty"`
	Metadata *testMetricsMetadata   `json:"metadata,omitempty"`
}

func TestUnmarshalMetricsResponse_WithTimeBucket(t *testing.T) {
	// This is the exact JSON shape returned by the API when granularity is specified.
	responseJSON := `{
		"data": [
			{
				"time_bucket": "2026-03-02T14:00:00Z",
				"dimensions": {"topic": "user.created"},
				"metrics": {"count": 1423}
			},
			{
				"time_bucket": "2026-03-02T15:00:00Z",
				"dimensions": {"topic": "user.created"},
				"metrics": {"count": 1891}
			}
		],
		"metadata": {
			"granularity": "1h",
			"query_time_ms": 5,
			"row_count": 2,
			"row_limit": 1000,
			"truncated": false
		}
	}`

	var out testMetricsResponse
	err := UnmarshalJSON([]byte(responseJSON), &out, "", true, nil)
	require.NoError(t, err, "unmarshalling metrics response with time_bucket should succeed")

	// Verify data points
	require.Len(t, out.Data, 2)

	// First data point
	require.NotNil(t, out.Data[0].TimeBucket)
	assert.Equal(t, time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC), *out.Data[0].TimeBucket)
	assert.Equal(t, "user.created", out.Data[0].Dimensions["topic"])

	// Second data point
	require.NotNil(t, out.Data[1].TimeBucket)
	assert.Equal(t, time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC), *out.Data[1].TimeBucket)
}

func TestUnmarshalMetricsResponse_WithoutTimeBucket(t *testing.T) {
	// When no granularity is specified, time_bucket is absent.
	responseJSON := `{
		"data": [
			{
				"dimensions": {},
				"metrics": {"count": 5000}
			}
		],
		"metadata": {
			"query_time_ms": 3,
			"row_count": 1,
			"row_limit": 1000,
			"truncated": false
		}
	}`

	var out testMetricsResponse
	err := UnmarshalJSON([]byte(responseJSON), &out, "", true, nil)
	require.NoError(t, err, "unmarshalling metrics response without time_bucket should succeed")

	require.Len(t, out.Data, 1)
	assert.Nil(t, out.Data[0].TimeBucket)
	assert.Nil(t, out.Metadata.Granularity)
}

func TestUnmarshalMetricsResponse_WithNullTimeBucket(t *testing.T) {
	// The API server currently returns "time_bucket": null (no omitempty on server side).
	// The SDK should handle this gracefully — null deserializes to nil *time.Time.
	responseJSON := `{
		"data": [
			{
				"time_bucket": null,
				"dimensions": {},
				"metrics": {"count": 5000}
			}
		],
		"metadata": {
			"granularity": null,
			"query_time_ms": 3,
			"row_count": 1,
			"row_limit": 1000,
			"truncated": false
		}
	}`

	var out testMetricsResponse
	err := UnmarshalJSON([]byte(responseJSON), &out, "", true, nil)
	require.NoError(t, err, "unmarshalling metrics response with null time_bucket should succeed")

	require.Len(t, out.Data, 1)
	assert.Nil(t, out.Data[0].TimeBucket)
	assert.Nil(t, out.Metadata.Granularity)
}

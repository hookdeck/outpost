package bucket

import (
	"sort"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

// FillEventBuckets fills in missing time buckets with zero-valued data points
// so that the response contains one entry per time slot (per dimension combo).
func FillEventBuckets(data []driver.EventMetricsDataPoint, req driver.MetricsRequest) []driver.EventMetricsDataPoint {
	if req.Granularity == nil {
		return data
	}

	slots := GenerateTimeBuckets(req.TimeRange.Start, req.TimeRange.End, req.Granularity)
	if len(slots) == 0 {
		return data
	}

	if len(req.Dimensions) == 0 {
		return fillEventNoDims(data, slots, req)
	}
	return fillEventWithDims(data, slots, req)
}

// FillAttemptBuckets fills in missing time buckets with zero-valued data points.
func FillAttemptBuckets(data []driver.AttemptMetricsDataPoint, req driver.MetricsRequest) []driver.AttemptMetricsDataPoint {
	if req.Granularity == nil {
		return data
	}

	slots := GenerateTimeBuckets(req.TimeRange.Start, req.TimeRange.End, req.Granularity)
	if len(slots) == 0 {
		return data
	}

	if len(req.Dimensions) == 0 {
		return fillAttemptNoDims(data, slots, req)
	}
	return fillAttemptWithDims(data, slots, req)
}

// ── Event filling ─────────────────────────────────────────────────────────

func fillEventNoDims(data []driver.EventMetricsDataPoint, slots []time.Time, req driver.MetricsRequest) []driver.EventMetricsDataPoint {
	index := map[time.Time]driver.EventMetricsDataPoint{}
	for _, dp := range data {
		if dp.TimeBucket != nil {
			index[*dp.TimeBucket] = dp
		}
	}

	result := make([]driver.EventMetricsDataPoint, 0, len(slots))
	for _, slot := range slots {
		if dp, ok := index[slot]; ok {
			result = append(result, dp)
		} else {
			result = append(result, zeroEventDP(slot, req.Measures))
		}
	}
	return result
}

func fillEventWithDims(data []driver.EventMetricsDataPoint, slots []time.Time, req driver.MetricsRequest) []driver.EventMetricsDataPoint {
	type key struct {
		dim  DimKey
		slot time.Time
	}

	// Collect unique dimension combos and a template for each.
	templates := map[DimKey]driver.EventMetricsDataPoint{}
	dimOrder := []DimKey{}
	index := map[key]driver.EventMetricsDataPoint{}

	for _, dp := range data {
		dk := EventDimKey(&dp, req.Dimensions)
		if _, exists := templates[dk]; !exists {
			templates[dk] = dp
			dimOrder = append(dimOrder, dk)
		}
		if dp.TimeBucket != nil {
			index[key{dk, *dp.TimeBucket}] = dp
		}
	}

	sort.Slice(dimOrder, func(i, j int) bool {
		return string(dimOrder[i]) < string(dimOrder[j])
	})

	result := make([]driver.EventMetricsDataPoint, 0, len(dimOrder)*len(slots))
	for _, dk := range dimOrder {
		tmpl := templates[dk]
		for _, slot := range slots {
			if dp, ok := index[key{dk, slot}]; ok {
				result = append(result, dp)
			} else {
				dp := zeroEventDP(slot, req.Measures)
				copyEventDims(&dp, &tmpl, req.Dimensions)
				result = append(result, dp)
			}
		}
	}
	return result
}

func zeroEventDP(slot time.Time, measures []string) driver.EventMetricsDataPoint {
	dp := driver.EventMetricsDataPoint{TimeBucket: new(slot)}
	for _, m := range measures {
		switch m {
		case "count":
			dp.Count = new(0)
		}
	}
	return dp
}

func copyEventDims(dst, src *driver.EventMetricsDataPoint, dims []string) {
	for _, dim := range dims {
		switch dim {
		case "tenant_id":
			if src.TenantID != nil {
				dst.TenantID = new(*src.TenantID)
			}
		case "topic":
			if src.Topic != nil {
				dst.Topic = new(*src.Topic)
			}
		case "destination_id":
			if src.DestinationID != nil {
				dst.DestinationID = new(*src.DestinationID)
			}
		case "eligible_for_retry":
			if src.EligibleForRetry != nil {
				dst.EligibleForRetry = new(*src.EligibleForRetry)
			}
		}
	}
}

// ── Attempt filling ───────────────────────────────────────────────────────

func fillAttemptNoDims(data []driver.AttemptMetricsDataPoint, slots []time.Time, req driver.MetricsRequest) []driver.AttemptMetricsDataPoint {
	index := map[time.Time]driver.AttemptMetricsDataPoint{}
	for _, dp := range data {
		if dp.TimeBucket != nil {
			index[*dp.TimeBucket] = dp
		}
	}

	result := make([]driver.AttemptMetricsDataPoint, 0, len(slots))
	for _, slot := range slots {
		if dp, ok := index[slot]; ok {
			result = append(result, dp)
		} else {
			result = append(result, zeroAttemptDP(slot, req.Measures))
		}
	}
	return result
}

func fillAttemptWithDims(data []driver.AttemptMetricsDataPoint, slots []time.Time, req driver.MetricsRequest) []driver.AttemptMetricsDataPoint {
	type key struct {
		dim  DimKey
		slot time.Time
	}

	templates := map[DimKey]driver.AttemptMetricsDataPoint{}
	dimOrder := []DimKey{}
	index := map[key]driver.AttemptMetricsDataPoint{}

	for _, dp := range data {
		dk := AttemptDimKey(&dp, req.Dimensions)
		if _, exists := templates[dk]; !exists {
			templates[dk] = dp
			dimOrder = append(dimOrder, dk)
		}
		if dp.TimeBucket != nil {
			index[key{dk, *dp.TimeBucket}] = dp
		}
	}

	sort.Slice(dimOrder, func(i, j int) bool {
		return string(dimOrder[i]) < string(dimOrder[j])
	})

	result := make([]driver.AttemptMetricsDataPoint, 0, len(dimOrder)*len(slots))
	for _, dk := range dimOrder {
		tmpl := templates[dk]
		for _, slot := range slots {
			if dp, ok := index[key{dk, slot}]; ok {
				result = append(result, dp)
			} else {
				dp := zeroAttemptDP(slot, req.Measures)
				copyAttemptDims(&dp, &tmpl, req.Dimensions)
				result = append(result, dp)
			}
		}
	}
	return result
}

func zeroAttemptDP(slot time.Time, measures []string) driver.AttemptMetricsDataPoint {
	dp := driver.AttemptMetricsDataPoint{TimeBucket: new(slot)}
	for _, m := range measures {
		switch m {
		case "count":
			dp.Count = new(0)
		case "successful_count":
			dp.SuccessfulCount = new(0)
		case "failed_count":
			dp.FailedCount = new(0)
		case "error_rate":
			dp.ErrorRate = new(0.0)
		case "first_attempt_count":
			dp.FirstAttemptCount = new(0)
		case "retry_count":
			dp.RetryCount = new(0)
		case "manual_retry_count":
			dp.ManualRetryCount = new(0)
		case "avg_attempt_number":
			dp.AvgAttemptNumber = new(0.0)
		}
	}
	return dp
}

func copyAttemptDims(dst, src *driver.AttemptMetricsDataPoint, dims []string) {
	for _, dim := range dims {
		switch dim {
		case "tenant_id":
			if src.TenantID != nil {
				dst.TenantID = new(*src.TenantID)
			}
		case "destination_id":
			if src.DestinationID != nil {
				dst.DestinationID = new(*src.DestinationID)
			}
		case "topic":
			if src.Topic != nil {
				dst.Topic = new(*src.Topic)
			}
		case "status":
			if src.Status != nil {
				dst.Status = new(*src.Status)
			}
		case "code":
			if src.Code != nil {
				dst.Code = new(*src.Code)
			}
		case "manual":
			if src.Manual != nil {
				dst.Manual = new(*src.Manual)
			}
		case "attempt_number":
			if src.AttemptNumber != nil {
				dst.AttemptNumber = new(*src.AttemptNumber)
			}
		}
	}
}

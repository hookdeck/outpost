package memlogstore

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/bucket"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
)

const defaultRowLimit = 100000

func (s *memLogStore) QueryEventMetrics(ctx context.Context, req driver.MetricsRequest) (*driver.EventMetricsResponse, error) {
	if err := driver.ValidateMetricsRequest(req); err != nil {
		return nil, err
	}
	req.Measures = driver.EnrichMeasuresForRates(req.Measures)
	s.mu.RLock()
	defer s.mu.RUnlock()

	start := time.Now()

	// Filter events
	var matched []*models.Event
	for _, event := range s.events {
		if !matchesEventMetricsFilter(event, req) {
			continue
		}
		matched = append(matched, event)
	}

	// Group by dimensions + time bucket
	type groupKey struct {
		timeBucket string
		tenantID   string
		topic      string
		destID     string
	}

	groups := map[groupKey][]*models.Event{}
	for _, event := range matched {
		key := groupKey{}
		if req.Granularity != nil {
			tb := bucket.TruncateTime(event.Time, req.Granularity)
			key.timeBucket = tb.Format(time.RFC3339)
		}
		for _, dim := range req.Dimensions {
			switch dim {
			case "tenant_id":
				key.tenantID = event.TenantID
			case "topic":
				key.topic = event.Topic
			case "destination_id":
				key.destID = event.DestinationID
			}
		}
		groups[key] = append(groups[key], event)
	}

	// Build response
	var data []driver.EventMetricsDataPoint
	for key, events := range groups {
		dp := driver.EventMetricsDataPoint{}

		if key.timeBucket != "" {
			tb, _ := time.Parse(time.RFC3339, key.timeBucket)
			dp.TimeBucket = &tb
		}

		// Dimensions
		for _, dim := range req.Dimensions {
			switch dim {
			case "tenant_id":
				v := key.tenantID
				dp.TenantID = &v
			case "topic":
				v := key.topic
				dp.Topic = &v
			case "destination_id":
				v := key.destID
				dp.DestinationID = &v
			}
		}

		// Measures
		for _, measure := range req.Measures {
			switch measure {
			case "count":
				c := len(events)
				dp.Count = &c
			}
		}

		data = append(data, dp)
	}

	// Handle empty result — no groups means no matching data
	if len(groups) == 0 {
		data = []driver.EventMetricsDataPoint{}
	}

	data = bucket.FillEventBuckets(data, req)
	driver.ComputeEventRates(data, req)

	elapsed := time.Since(start)
	return &driver.EventMetricsResponse{
		Data: data,
		Metadata: driver.MetricsMetadata{
			QueryTimeMs: elapsed.Milliseconds(),
			RowCount:    len(data),
			RowLimit:    defaultRowLimit,
			Truncated:   false,
		},
	}, nil
}

func (s *memLogStore) QueryAttemptMetrics(ctx context.Context, req driver.MetricsRequest) (*driver.AttemptMetricsResponse, error) {
	if err := driver.ValidateMetricsRequest(req); err != nil {
		return nil, err
	}
	req.Measures = driver.EnrichMeasuresForRates(req.Measures)
	s.mu.RLock()
	defer s.mu.RUnlock()

	start := time.Now()

	var matched []attemptWithEvent
	for _, a := range s.attempts {
		event := s.events[a.EventID]
		if event == nil {
			continue
		}
		if !matchesAttemptMetricsFilter(a, event, req) {
			continue
		}
		matched = append(matched, attemptWithEvent{attempt: a, event: event})
	}

	// Group by dimensions + time bucket
	type groupKey struct {
		timeBucket string
		tenantID   string
		destID     string
		topic      string
		status     string
		code       string
		manual     string
		attemptNum string
	}

	groups := map[groupKey][]attemptWithEvent{}
	for _, ae := range matched {
		key := groupKey{}
		if req.Granularity != nil {
			tb := bucket.TruncateTime(ae.attempt.Time, req.Granularity)
			key.timeBucket = tb.Format(time.RFC3339)
		}
		for _, dim := range req.Dimensions {
			switch dim {
			case "tenant_id":
				key.tenantID = ae.event.TenantID
			case "destination_id":
				key.destID = ae.attempt.DestinationID
			case "topic":
				key.topic = ae.event.Topic
			case "status":
				key.status = ae.attempt.Status
			case "code":
				key.code = ae.attempt.Code
			case "manual":
				if ae.attempt.Manual {
					key.manual = "true"
				} else {
					key.manual = "false"
				}
			case "attempt_number":
				key.attemptNum = fmt.Sprintf("%d", ae.attempt.AttemptNumber)
			}
		}
		groups[key] = append(groups[key], ae)
	}

	// Build response
	var data []driver.AttemptMetricsDataPoint
	for key, attempts := range groups {
		dp := driver.AttemptMetricsDataPoint{}

		if key.timeBucket != "" {
			tb, _ := time.Parse(time.RFC3339, key.timeBucket)
			dp.TimeBucket = &tb
		}

		// Dimensions
		for _, dim := range req.Dimensions {
			switch dim {
			case "tenant_id":
				v := key.tenantID
				dp.TenantID = &v
			case "destination_id":
				v := key.destID
				dp.DestinationID = &v
			case "topic":
				v := key.topic
				dp.Topic = &v
			case "status":
				v := key.status
				dp.Status = &v
			case "code":
				v := key.code
				dp.Code = &v
			case "manual":
				v := key.manual == "true"
				dp.Manual = &v
			case "attempt_number":
				v := attempts[0].attempt.AttemptNumber
				dp.AttemptNumber = &v
			}
		}

		// Measures
		for _, measure := range req.Measures {
			switch measure {
			case "count":
				c := len(attempts)
				dp.Count = &c
			case "successful_count":
				c := countByStatus(attempts, "success")
				dp.SuccessfulCount = &c
			case "failed_count":
				c := countByStatus(attempts, "failed")
				dp.FailedCount = &c
			case "error_rate":
				total := len(attempts)
				failed := countByStatus(attempts, "failed")
				var rate float64
				if total > 0 {
					rate = float64(failed) / float64(total)
				}
				dp.ErrorRate = &rate
			case "first_attempt_count":
				c := 0
				for _, ae := range attempts {
					if ae.attempt.AttemptNumber == 0 {
						c++
					}
				}
				dp.FirstAttemptCount = &c
			case "retry_count":
				c := 0
				for _, ae := range attempts {
					if ae.attempt.AttemptNumber > 0 {
						c++
					}
				}
				dp.RetryCount = &c
			case "manual_retry_count":
				c := 0
				for _, ae := range attempts {
					if ae.attempt.Manual {
						c++
					}
				}
				dp.ManualRetryCount = &c
			case "avg_attempt_number":
				total := 0
				for _, ae := range attempts {
					total += ae.attempt.AttemptNumber
				}
				var avg float64
				if len(attempts) > 0 {
					avg = float64(total) / float64(len(attempts))
				}
				dp.AvgAttemptNumber = &avg
			}
		}

		data = append(data, dp)
	}

	if len(groups) == 0 {
		data = []driver.AttemptMetricsDataPoint{}
	}

	data = bucket.FillAttemptBuckets(data, req)
	driver.ComputeAttemptRates(data, req)

	elapsed := time.Since(start)
	return &driver.AttemptMetricsResponse{
		Data: data,
		Metadata: driver.MetricsMetadata{
			QueryTimeMs: elapsed.Milliseconds(),
			RowCount:    len(data),
			RowLimit:    defaultRowLimit,
			Truncated:   false,
		},
	}, nil
}

func matchesEventMetricsFilter(event *models.Event, req driver.MetricsRequest) bool {
	if req.TenantID != "" && event.TenantID != req.TenantID {
		return false
	}
	if event.Time.Before(req.TimeRange.Start) || !event.Time.Before(req.TimeRange.End) {
		return false
	}
	if topics, ok := req.Filters["topic"]; ok {
		if !contains(topics, event.Topic) {
			return false
		}
	}
	if dests, ok := req.Filters["destination_id"]; ok {
		if !contains(dests, event.DestinationID) {
			return false
		}
	}
	return true
}

func matchesAttemptMetricsFilter(a *models.Attempt, event *models.Event, req driver.MetricsRequest) bool {
	if req.TenantID != "" && event.TenantID != req.TenantID {
		return false
	}
	if a.Time.Before(req.TimeRange.Start) || !a.Time.Before(req.TimeRange.End) {
		return false
	}
	if statuses, ok := req.Filters["status"]; ok {
		if !contains(statuses, a.Status) {
			return false
		}
	}
	if dests, ok := req.Filters["destination_id"]; ok {
		if !contains(dests, a.DestinationID) {
			return false
		}
	}
	if topics, ok := req.Filters["topic"]; ok {
		if !contains(topics, event.Topic) {
			return false
		}
	}
	if codes, ok := req.Filters["code"]; ok {
		if !contains(codes, a.Code) {
			return false
		}
	}
	if manuals, ok := req.Filters["manual"]; ok {
		manualStr := "false"
		if a.Manual {
			manualStr = "true"
		}
		if !contains(manuals, manualStr) {
			return false
		}
	}
	if attemptNums, ok := req.Filters["attempt_number"]; ok {
		if !contains(attemptNums, fmt.Sprintf("%d", a.AttemptNumber)) {
			return false
		}
	}
	return true
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

func countByStatus(attempts []attemptWithEvent, status string) int {
	c := 0
	for _, ae := range attempts {
		if ae.attempt.Status == status {
			c++
		}
	}
	return c
}

type attemptWithEvent struct {
	attempt *models.Attempt
	event   *models.Event
}

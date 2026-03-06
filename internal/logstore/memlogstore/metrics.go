package memlogstore

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
)

const defaultRowLimit = 100000

func (s *memLogStore) QueryEventMetrics(ctx context.Context, req driver.MetricsRequest) (*driver.EventMetricsResponse, error) {
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
		eligible   string
	}

	groups := map[groupKey][]*models.Event{}
	for _, event := range matched {
		key := groupKey{}
		if req.Granularity != nil {
			tb := truncateTime(event.Time, req.Granularity)
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
			case "eligible_for_retry":
				if event.EligibleForRetry {
					key.eligible = "true"
				} else {
					key.eligible = "false"
				}
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
			case "eligible_for_retry":
				v := key.eligible == "true"
				dp.EligibleForRetry = &v
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
			tb := truncateTime(ae.attempt.Time, req.Granularity)
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
	if event.Time.Before(req.DateRange.Start) || !event.Time.Before(req.DateRange.End) {
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
	if a.Time.Before(req.DateRange.Start) || !a.Time.Before(req.DateRange.End) {
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

func truncateTime(t time.Time, g *driver.Granularity) time.Time {
	t = t.UTC()
	switch g.Unit {
	case "s":
		d := time.Duration(g.Value) * time.Second
		return t.Truncate(d)
	case "m":
		d := time.Duration(g.Value) * time.Minute
		return t.Truncate(d)
	case "h":
		d := time.Duration(g.Value) * time.Hour
		return t.Truncate(d)
	case "d":
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	case "w":
		weekday := int(t.Weekday())
		return time.Date(t.Year(), t.Month(), t.Day()-weekday, 0, 0, 0, 0, time.UTC)
	case "M":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		return t
	}
}

type attemptWithEvent struct {
	attempt *models.Attempt
	event   *models.Event
}

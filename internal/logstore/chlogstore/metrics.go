package chlogstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/bucket"
	"github.com/hookdeck/outpost/internal/logstore/driver"
)

const (
	defaultRowLimit     = 100000
	metricsQueryTimeout = 30 * time.Second
)

func metricsCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, metricsQueryTimeout)
}

// chTimeBucketExpr returns a ClickHouse expression that truncates a DateTime64
// column to the given granularity, matching the Go truncation semantics used
// in the in-memory driver.
func chTimeBucketExpr(col string, g *driver.Granularity) string {
	switch g.Unit {
	case "s":
		return fmt.Sprintf("toStartOfInterval(%s, INTERVAL %d SECOND)", col, g.Value)
	case "m":
		return fmt.Sprintf("toStartOfInterval(%s, INTERVAL %d MINUTE)", col, g.Value)
	case "h":
		return fmt.Sprintf("toStartOfInterval(%s, INTERVAL %d HOUR)", col, g.Value)
	case "d":
		return fmt.Sprintf("toStartOfDay(%s)", col)
	case "w":
		// mode 0 = Sunday-based weeks, matching Go's time.Weekday convention.
		return fmt.Sprintf("toStartOfWeek(%s, 0)", col)
	case "M":
		return fmt.Sprintf("toStartOfMonth(%s)", col)
	default:
		return col
	}
}

// addInFilter appends an IN condition with individual ? placeholders.
func addInFilter(conditions []string, args []any, col string, vals []string) ([]string, []any) {
	if len(vals) == 0 {
		return conditions, args
	}
	conditions = append(conditions, col+" IN ?")
	args = append(args, vals)
	return conditions, args
}

// ── Event Metrics ─────────────────────────────────────────────────────────

func (s *logStoreImpl) QueryEventMetrics(ctx context.Context, req driver.MetricsRequest) (*driver.EventMetricsResponse, error) {
	ctx, cancel := metricsCtx(ctx)
	defer cancel()

	start := time.Now()

	var (
		selectExprs []string
		groupExprs  []string
		conditions  []string
		args        []any
	)

	type sf int
	const (
		sfTimeBucket sf = iota
		sfTopic
		sfDestID
		sfEligible
		sfCount
	)
	var order []sf

	// Time bucket
	if req.Granularity != nil {
		expr := chTimeBucketExpr("event_time", req.Granularity)
		selectExprs = append(selectExprs, expr+" AS time_bucket")
		groupExprs = append(groupExprs, expr)
		order = append(order, sfTimeBucket)
	}

	// Dimensions
	for _, dim := range req.Dimensions {
		switch dim {
		case "topic":
			selectExprs = append(selectExprs, "topic")
			groupExprs = append(groupExprs, "topic")
			order = append(order, sfTopic)
		case "destination_id":
			selectExprs = append(selectExprs, "destination_id")
			groupExprs = append(groupExprs, "destination_id")
			order = append(order, sfDestID)
		case "eligible_for_retry":
			selectExprs = append(selectExprs, "eligible_for_retry")
			groupExprs = append(groupExprs, "eligible_for_retry")
			order = append(order, sfEligible)
		}
	}

	// Measures — use uniqExact(event_id) instead of count() to handle
	// ReplacingMergeTree duplicates from unmerged parts without FINAL.
	for _, measure := range req.Measures {
		switch measure {
		case "count":
			selectExprs = append(selectExprs, "uniqExact(event_id)")
			order = append(order, sfCount)
		}
	}

	// WHERE
	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}
	conditions = append(conditions, "event_time >= ?")
	args = append(args, req.DateRange.Start)
	conditions = append(conditions, "event_time < ?")
	args = append(args, req.DateRange.End)

	if topics, ok := req.Filters["topic"]; ok {
		conditions, args = addInFilter(conditions, args, "topic", topics)
	}
	if dests, ok := req.Filters["destination_id"]; ok {
		conditions, args = addInFilter(conditions, args, "destination_id", dests)
	}

	// Build SQL — no FINAL needed; uniqExact(event_id) handles dedup from
	// unmerged ReplacingMergeTree parts.
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s",
		strings.Join(selectExprs, ", "),
		s.eventsTable,
		strings.Join(conditions, " AND "))
	if len(groupExprs) > 0 {
		query += " GROUP BY " + strings.Join(groupExprs, ", ")
	}
	query += " HAVING count() > 0"
	if len(groupExprs) > 0 {
		query += " ORDER BY " + strings.Join(groupExprs, ", ")
	}
	query += fmt.Sprintf(" LIMIT %d", defaultRowLimit+1)

	rows, err := s.chDB.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query event metrics: %w", err)
	}
	defer rows.Close()

	var (
		tbVal       time.Time
		topicVal    string
		destIDVal   string
		eligibleVal bool
		countVal    uint64
	)
	scanDests := make([]any, len(order))
	for i, f := range order {
		switch f {
		case sfTimeBucket:
			scanDests[i] = &tbVal
		case sfTopic:
			scanDests[i] = &topicVal
		case sfDestID:
			scanDests[i] = &destIDVal
		case sfEligible:
			scanDests[i] = &eligibleVal
		case sfCount:
			scanDests[i] = &countVal
		}
	}

	data := []driver.EventMetricsDataPoint{}
	for rows.Next() {
		if err := rows.Scan(scanDests...); err != nil {
			return nil, fmt.Errorf("scan event metrics: %w", err)
		}

		dp := driver.EventMetricsDataPoint{}
		for _, f := range order {
			switch f {
			case sfTimeBucket:
				t := tbVal.UTC()
				dp.TimeBucket = &t
			case sfTopic:
				v := topicVal
				dp.Topic = &v
			case sfDestID:
				v := destIDVal
				dp.DestinationID = &v
			case sfEligible:
				v := eligibleVal
				dp.EligibleForRetry = &v
			case sfCount:
				v := int(countVal)
				dp.Count = &v
			}
		}
		data = append(data, dp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	truncated := len(data) > defaultRowLimit
	if truncated {
		data = data[:defaultRowLimit]
	}

	data = bucket.FillEventBuckets(data, req)

	elapsed := time.Since(start)
	return &driver.EventMetricsResponse{
		Data: data,
		Metadata: driver.MetricsMetadata{
			QueryTimeMs: elapsed.Milliseconds(),
			RowCount:    len(data),
			RowLimit:    defaultRowLimit,
			Truncated:   truncated,
		},
	}, nil
}

// ── Attempt Metrics ───────────────────────────────────────────────────────

func (s *logStoreImpl) QueryAttemptMetrics(ctx context.Context, req driver.MetricsRequest) (*driver.AttemptMetricsResponse, error) {
	ctx, cancel := metricsCtx(ctx)
	defer cancel()

	start := time.Now()

	var (
		selectExprs []string
		groupExprs  []string
		conditions  []string
		args        []any
	)

	type sf int
	const (
		sfTimeBucket sf = iota
		sfDestID
		sfTopic
		sfStatus
		sfCode
		sfManual
		sfCount
		sfSuccessCount
		sfFailedCount
		sfErrorRate
		sfFirstAttempt
		sfRetryCount
		sfManualRetry
		sfAvgAttemptNum
	)
	var order []sf

	// Time bucket
	if req.Granularity != nil {
		expr := chTimeBucketExpr("attempt_time", req.Granularity)
		selectExprs = append(selectExprs, expr+" AS time_bucket")
		groupExprs = append(groupExprs, expr)
		order = append(order, sfTimeBucket)
	}

	// Dimensions
	for _, dim := range req.Dimensions {
		switch dim {
		case "destination_id":
			selectExprs = append(selectExprs, "destination_id")
			groupExprs = append(groupExprs, "destination_id")
			order = append(order, sfDestID)
		case "topic":
			selectExprs = append(selectExprs, "topic")
			groupExprs = append(groupExprs, "topic")
			order = append(order, sfTopic)
		case "status":
			selectExprs = append(selectExprs, "status")
			groupExprs = append(groupExprs, "status")
			order = append(order, sfStatus)
		case "code":
			selectExprs = append(selectExprs, "code")
			groupExprs = append(groupExprs, "code")
			order = append(order, sfCode)
		case "manual":
			selectExprs = append(selectExprs, "manual")
			groupExprs = append(groupExprs, "manual")
			order = append(order, sfManual)
		}
	}

	// Measures — use uniqExact/uniqExactIf(attempt_id, ...) instead of
	// count/countIf to handle ReplacingMergeTree duplicates without FINAL.
	// avg(attempt_number) is kept as-is: duplicates have identical values,
	// so the average is only negligibly affected during brief merge windows.
	for _, measure := range req.Measures {
		switch measure {
		case "count":
			selectExprs = append(selectExprs, "uniqExact(attempt_id)")
			order = append(order, sfCount)
		case "successful_count":
			selectExprs = append(selectExprs, "uniqExactIf(attempt_id, status = 'success')")
			order = append(order, sfSuccessCount)
		case "failed_count":
			selectExprs = append(selectExprs, "uniqExactIf(attempt_id, status = 'failed')")
			order = append(order, sfFailedCount)
		case "error_rate":
			selectExprs = append(selectExprs, "uniqExactIf(attempt_id, status = 'failed') / uniqExact(attempt_id)")
			order = append(order, sfErrorRate)
		case "first_attempt_count":
			selectExprs = append(selectExprs, "uniqExactIf(attempt_id, attempt_number = 0)")
			order = append(order, sfFirstAttempt)
		case "retry_count":
			selectExprs = append(selectExprs, "uniqExactIf(attempt_id, attempt_number > 0)")
			order = append(order, sfRetryCount)
		case "manual_retry_count":
			selectExprs = append(selectExprs, "uniqExactIf(attempt_id, manual)")
			order = append(order, sfManualRetry)
		case "avg_attempt_number":
			selectExprs = append(selectExprs, "avg(attempt_number)")
			order = append(order, sfAvgAttemptNum)
		}
	}

	// WHERE
	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = ?")
		args = append(args, req.TenantID)
	}
	conditions = append(conditions, "attempt_time >= ?")
	args = append(args, req.DateRange.Start)
	conditions = append(conditions, "attempt_time < ?")
	args = append(args, req.DateRange.End)

	if statuses, ok := req.Filters["status"]; ok {
		conditions, args = addInFilter(conditions, args, "status", statuses)
	}
	if dests, ok := req.Filters["destination_id"]; ok {
		conditions, args = addInFilter(conditions, args, "destination_id", dests)
	}
	if topics, ok := req.Filters["topic"]; ok {
		conditions, args = addInFilter(conditions, args, "topic", topics)
	}
	if codes, ok := req.Filters["code"]; ok {
		conditions, args = addInFilter(conditions, args, "code", codes)
	}
	if manuals, ok := req.Filters["manual"]; ok {
		conditions, args = addInFilter(conditions, args, "manual", manuals)
	}
	if attemptNums, ok := req.Filters["attempt_number"]; ok {
		conditions, args = addInFilter(conditions, args, "attempt_number", attemptNums)
	}

	// Build SQL
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s",
		strings.Join(selectExprs, ", "),
		s.attemptsTable,
		strings.Join(conditions, " AND "))
	if len(groupExprs) > 0 {
		query += " GROUP BY " + strings.Join(groupExprs, ", ")
	}
	query += " HAVING count() > 0"
	if len(groupExprs) > 0 {
		query += " ORDER BY " + strings.Join(groupExprs, ", ")
	}
	query += fmt.Sprintf(" LIMIT %d", defaultRowLimit+1)

	rows, err := s.chDB.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query attempt metrics: %w", err)
	}
	defer rows.Close()

	var (
		tbVal         time.Time
		destIDVal     string
		topicVal      string
		statusVal     string
		codeVal       string
		manualVal     bool
		countVal      uint64
		successCount  uint64
		failedCount   uint64
		errorRate     float64
		firstAttempt  uint64
		retryCount    uint64
		manualRetry   uint64
		avgAttemptNum float64
	)

	scanDests := make([]any, len(order))
	for i, f := range order {
		switch f {
		case sfTimeBucket:
			scanDests[i] = &tbVal
		case sfDestID:
			scanDests[i] = &destIDVal
		case sfTopic:
			scanDests[i] = &topicVal
		case sfStatus:
			scanDests[i] = &statusVal
		case sfCode:
			scanDests[i] = &codeVal
		case sfManual:
			scanDests[i] = &manualVal
		case sfCount:
			scanDests[i] = &countVal
		case sfSuccessCount:
			scanDests[i] = &successCount
		case sfFailedCount:
			scanDests[i] = &failedCount
		case sfErrorRate:
			scanDests[i] = &errorRate
		case sfFirstAttempt:
			scanDests[i] = &firstAttempt
		case sfRetryCount:
			scanDests[i] = &retryCount
		case sfManualRetry:
			scanDests[i] = &manualRetry
		case sfAvgAttemptNum:
			scanDests[i] = &avgAttemptNum
		}
	}

	data := []driver.AttemptMetricsDataPoint{}
	for rows.Next() {
		if err := rows.Scan(scanDests...); err != nil {
			return nil, fmt.Errorf("scan attempt metrics: %w", err)
		}

		dp := driver.AttemptMetricsDataPoint{}
		for _, f := range order {
			switch f {
			case sfTimeBucket:
				t := tbVal.UTC()
				dp.TimeBucket = &t
			case sfDestID:
				v := destIDVal
				dp.DestinationID = &v
			case sfTopic:
				v := topicVal
				dp.Topic = &v
			case sfStatus:
				v := statusVal
				dp.Status = &v
			case sfCode:
				v := codeVal
				dp.Code = &v
			case sfManual:
				v := manualVal
				dp.Manual = &v
			case sfCount:
				v := int(countVal)
				dp.Count = &v
			case sfSuccessCount:
				v := int(successCount)
				dp.SuccessfulCount = &v
			case sfFailedCount:
				v := int(failedCount)
				dp.FailedCount = &v
			case sfErrorRate:
				v := errorRate
				dp.ErrorRate = &v
			case sfFirstAttempt:
				v := int(firstAttempt)
				dp.FirstAttemptCount = &v
			case sfRetryCount:
				v := int(retryCount)
				dp.RetryCount = &v
			case sfManualRetry:
				v := int(manualRetry)
				dp.ManualRetryCount = &v
			case sfAvgAttemptNum:
				v := avgAttemptNum
				dp.AvgAttemptNumber = &v
			}
		}
		data = append(data, dp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	truncated := len(data) > defaultRowLimit
	if truncated {
		data = data[:defaultRowLimit]
	}

	data = bucket.FillAttemptBuckets(data, req)

	elapsed := time.Since(start)
	return &driver.AttemptMetricsResponse{
		Data: data,
		Metadata: driver.MetricsMetadata{
			QueryTimeMs: elapsed.Milliseconds(),
			RowCount:    len(data),
			RowLimit:    defaultRowLimit,
			Truncated:   truncated,
		},
	}, nil
}

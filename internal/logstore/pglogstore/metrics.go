package pglogstore

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

// metricsCtx adds a fallback timeout to the context if the caller didn't set
// one. When the deadline fires, pgx cancels the running statement on the
// PostgreSQL side via pg_cancel_backend.
func metricsCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {} // caller already set a deadline
	}
	return context.WithTimeout(ctx, metricsQueryTimeout)
}

// timeBucketExpr returns a PostgreSQL expression that truncates a timestamptz
// column to the given granularity, matching the Go truncation semantics used
// in the in-memory driver.
func timeBucketExpr(col string, g *driver.Granularity) string {
	switch g.Unit {
	case "s":
		return fmt.Sprintf("date_bin('%d seconds'::interval, %s, '2000-01-01T00:00:00Z'::timestamptz)", g.Value, col)
	case "m":
		return fmt.Sprintf("date_bin('%d minutes'::interval, %s, '2000-01-01T00:00:00Z'::timestamptz)", g.Value, col)
	case "h":
		return fmt.Sprintf("date_bin('%d hours'::interval, %s, '2000-01-01T00:00:00Z'::timestamptz)", g.Value, col)
	case "d":
		return fmt.Sprintf("date_trunc('day', %s AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'", col)
	case "w":
		// Sunday-based weeks. 2000-01-02 is a Sunday, anchoring 7-day bins
		// to week boundaries that start on Sunday (matching Go's time.Weekday).
		return fmt.Sprintf("date_bin('7 days'::interval, %s, '2000-01-02T00:00:00Z'::timestamptz)", col)
	case "M":
		return fmt.Sprintf("date_trunc('month', %s AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'", col)
	default:
		return col
	}
}

// ── Event Metrics ─────────────────────────────────────────────────────────

func (s *logStore) QueryEventMetrics(ctx context.Context, req driver.MetricsRequest) (*driver.EventMetricsResponse, error) {
	ctx, cancel := metricsCtx(ctx)
	defer cancel()

	start := time.Now()

	var (
		selectExprs []string
		groupExprs  []string
		conditions  []string
		args        []any
		argNum      int
	)

	arg := func(v any) string {
		argNum++
		args = append(args, v)
		return fmt.Sprintf("$%d", argNum)
	}

	// Track which fields appear in each result row so we can build matching
	// scan destinations and map them back onto the data-point struct.
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
		expr := timeBucketExpr("time", req.Granularity)
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

	// Measures
	for _, measure := range req.Measures {
		switch measure {
		case "count":
			selectExprs = append(selectExprs, "COUNT(*)")
			order = append(order, sfCount)
		}
	}

	// WHERE
	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = "+arg(req.TenantID))
	}
	conditions = append(conditions, "time >= "+arg(req.TimeRange.Start))
	conditions = append(conditions, "time < "+arg(req.TimeRange.End))

	if topics, ok := req.Filters["topic"]; ok {
		conditions = append(conditions, "topic = ANY("+arg(topics)+")")
	}
	if dests, ok := req.Filters["destination_id"]; ok {
		conditions = append(conditions, "destination_id = ANY("+arg(dests)+")")
	}

	// Build SQL
	query := "SELECT " + strings.Join(selectExprs, ", ") +
		" FROM events WHERE " + strings.Join(conditions, " AND ")
	if len(groupExprs) > 0 {
		query += " GROUP BY " + strings.Join(groupExprs, ", ")
	}
	query += " HAVING COUNT(*) > 0"
	if len(groupExprs) > 0 {
		query += " ORDER BY " + strings.Join(groupExprs, ", ")
	}
	query += fmt.Sprintf(" LIMIT %d", defaultRowLimit+1)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query event metrics: %w", err)
	}
	defer rows.Close()

	// Prepare reusable scan destinations (one variable per possible field).
	var (
		tbVal       time.Time
		topicVal    string
		destIDVal   string
		eligibleVal bool
		countVal    int
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
				v := countVal
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

func (s *logStore) QueryAttemptMetrics(ctx context.Context, req driver.MetricsRequest) (*driver.AttemptMetricsResponse, error) {
	ctx, cancel := metricsCtx(ctx)
	defer cancel()

	start := time.Now()

	var (
		selectExprs []string
		groupExprs  []string
		conditions  []string
		args        []any
		argNum      int
	)

	arg := func(v any) string {
		argNum++
		args = append(args, v)
		return fmt.Sprintf("$%d", argNum)
	}

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
		expr := timeBucketExpr("time", req.Granularity)
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
			selectExprs = append(selectExprs, "COALESCE(code, '')")
			groupExprs = append(groupExprs, "code")
			order = append(order, sfCode)
		case "manual":
			selectExprs = append(selectExprs, "manual")
			groupExprs = append(groupExprs, "manual")
			order = append(order, sfManual)
		}
	}

	// Measures
	for _, measure := range req.Measures {
		switch measure {
		case "count":
			selectExprs = append(selectExprs, "COUNT(*)")
			order = append(order, sfCount)
		case "successful_count":
			selectExprs = append(selectExprs, "COUNT(*) FILTER (WHERE status = 'success')")
			order = append(order, sfSuccessCount)
		case "failed_count":
			selectExprs = append(selectExprs, "COUNT(*) FILTER (WHERE status = 'failed')")
			order = append(order, sfFailedCount)
		case "error_rate":
			selectExprs = append(selectExprs, "COUNT(*) FILTER (WHERE status = 'failed')::float8 / COUNT(*)")
			order = append(order, sfErrorRate)
		case "first_attempt_count":
			selectExprs = append(selectExprs, "COUNT(*) FILTER (WHERE attempt_number = 0)")
			order = append(order, sfFirstAttempt)
		case "retry_count":
			selectExprs = append(selectExprs, "COUNT(*) FILTER (WHERE attempt_number > 0)")
			order = append(order, sfRetryCount)
		case "manual_retry_count":
			selectExprs = append(selectExprs, "COUNT(*) FILTER (WHERE manual = true)")
			order = append(order, sfManualRetry)
		case "avg_attempt_number":
			selectExprs = append(selectExprs, "AVG(attempt_number)::float8")
			order = append(order, sfAvgAttemptNum)
		}
	}

	// WHERE
	if req.TenantID != "" {
		conditions = append(conditions, "tenant_id = "+arg(req.TenantID))
	}
	conditions = append(conditions, "time >= "+arg(req.TimeRange.Start))
	conditions = append(conditions, "time < "+arg(req.TimeRange.End))

	if statuses, ok := req.Filters["status"]; ok {
		conditions = append(conditions, "status = ANY("+arg(statuses)+")")
	}
	if dests, ok := req.Filters["destination_id"]; ok {
		conditions = append(conditions, "destination_id = ANY("+arg(dests)+")")
	}
	if topics, ok := req.Filters["topic"]; ok {
		conditions = append(conditions, "topic = ANY("+arg(topics)+")")
	}
	if codes, ok := req.Filters["code"]; ok {
		conditions = append(conditions, "code = ANY("+arg(codes)+")")
	}
	if manuals, ok := req.Filters["manual"]; ok {
		conditions = append(conditions, "manual = ANY("+arg(manuals)+"::boolean[])")
	}
	if attemptNums, ok := req.Filters["attempt_number"]; ok {
		conditions = append(conditions, "attempt_number = ANY("+arg(attemptNums)+"::integer[])")
	}

	// Build SQL
	query := "SELECT " + strings.Join(selectExprs, ", ") +
		" FROM attempts WHERE " + strings.Join(conditions, " AND ")
	if len(groupExprs) > 0 {
		query += " GROUP BY " + strings.Join(groupExprs, ", ")
	}
	query += " HAVING COUNT(*) > 0"
	if len(groupExprs) > 0 {
		query += " ORDER BY " + strings.Join(groupExprs, ", ")
	}
	query += fmt.Sprintf(" LIMIT %d", defaultRowLimit+1)

	rows, err := s.db.Query(ctx, query, args...)
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
		countVal      int
		successCount  int
		failedCount   int
		errorRate     float64
		firstAttempt  int
		retryCount    int
		manualRetry   int
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
				v := countVal
				dp.Count = &v
			case sfSuccessCount:
				v := successCount
				dp.SuccessfulCount = &v
			case sfFailedCount:
				v := failedCount
				dp.FailedCount = &v
			case sfErrorRate:
				v := errorRate
				dp.ErrorRate = &v
			case sfFirstAttempt:
				v := firstAttempt
				dp.FirstAttemptCount = &v
			case sfRetryCount:
				v := retryCount
				dp.RetryCount = &v
			case sfManualRetry:
				v := manualRetry
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

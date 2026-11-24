package chlogstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	chgo "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/models"
)

// ListLatestAttempts runs one query for all keys. The ClickHouse time bound is the
// oldest of the keys' bounds; each key's own bound is applied to the result.
// Lazy materialization lets the payload be read only for the returned rows.
func (s *logStoreImpl) ListLatestAttempts(ctx context.Context, lookups []driver.LatestAttemptLookup) (map[driver.AttemptKey]*driver.AttemptRecord, error) {
	notBefore := make(map[driver.AttemptKey]time.Time, len(lookups))
	for _, l := range lookups {
		if prev, ok := notBefore[l.Key]; !ok || l.NotBefore.Before(prev) {
			notBefore[l.Key] = l.NotBefore
		}
	}
	if len(notBefore) == 0 {
		return map[driver.AttemptKey]*driver.AttemptRecord{}, nil
	}

	var tenants, events, destinations []string
	var oldest time.Time
	bounded := true
	for key, nb := range notBefore {
		tenants = append(tenants, key.TenantID)
		events = append(events, key.EventID)
		destinations = append(destinations, key.DestinationID)
		if nb.IsZero() {
			bounded = false
		} else if oldest.IsZero() || nb.Before(oldest) {
			oldest = nb
		}
	}

	timeCond := ""
	var timeArgs []any
	if bounded {
		timeCond = " AND attempt_time >= ?"
		timeArgs = []any{oldest}
	}
	keyCond := "tenant_id IN ? AND event_id IN ? AND destination_id IN ?"
	keyArgs := []any{tenants, events, destinations}

	// The tuple filter is needed for exactness: the column filters alone match
	// other destinations of the same event. It sits in the subquery so the
	// outer query reads only the matched attempt ids. The outer limit leaves
	// room for duplicate rows of a not yet merged ReplacingMergeTree.
	query := fmt.Sprintf(`
		SELECT
			event_id, tenant_id, destination_id, destination_type, topic, eligible_for_retry,
			event_time, metadata, data, attempt_id, status, attempt_time, code, response_data,
			manual, attempt_number, latency_ms
		FROM %[1]s
		WHERE %[2]s%[3]s
		AND attempt_id IN (
			SELECT argMax(attempt_id, (attempt_time, attempt_id)) FROM %[1]s
			WHERE %[2]s
			AND (tenant_id, event_id, destination_id) IN (SELECT arrayJoin(arrayZip(?, ?, ?)))%[3]s
			GROUP BY tenant_id, event_id, destination_id
		)
		ORDER BY attempt_time DESC, attempt_id DESC
		LIMIT %[4]d
	`, s.attemptsTable, keyCond, timeCond, 2*len(notBefore))

	var args []any
	args = append(args, keyArgs...)
	args = append(args, timeArgs...)
	args = append(args, keyArgs...)
	args = append(args, tenants, events, destinations)
	args = append(args, timeArgs...)

	rows, err := s.queryLazy(ctx, query, args)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	records, err := scanAttemptRecords(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	latest := make(map[driver.AttemptKey]attemptRecordWithPosition, len(records))
	for _, r := range records {
		key := driver.AttemptKey{TenantID: r.Attempt.TenantID, EventID: r.Attempt.EventID, DestinationID: r.Attempt.DestinationID}
		if r.Attempt.Time.Before(notBefore[key]) {
			continue
		}
		if cur, ok := latest[key]; ok && !newerAttempt(r.Attempt, cur.Attempt) {
			continue
		}
		latest[key] = r
	}

	result := make(map[driver.AttemptKey]*driver.AttemptRecord, len(latest))
	var keyErrs driver.KeyErrors
	for key, r := range latest {
		if r.err != nil {
			if keyErrs == nil {
				keyErrs = driver.KeyErrors{}
			}
			keyErrs[key] = r.err
			continue
		}
		result[key] = r.AttemptRecord
	}

	// A full result may have lost keys to duplicate rows; look those up alone.
	if len(records) >= 2*len(notBefore) {
		for key, nb := range notBefore {
			if _, ok := latest[key]; ok {
				continue
			}
			req := driver.ListAttemptRequest{
				TenantIDs:      []string{key.TenantID},
				EventIDs:       []string{key.EventID},
				DestinationIDs: []string{key.DestinationID},
				Limit:          1,
				SortOrder:      "desc",
				Include:        driver.AllFields,
			}
			if !nb.IsZero() {
				req.TimeFilter.GTE = &nb
			}
			resp, err := s.ListAttempt(ctx, req)
			var decodeErr *decodeError
			if errors.As(err, &decodeErr) {
				if keyErrs == nil {
					keyErrs = driver.KeyErrors{}
				}
				keyErrs[key] = err
				continue
			}
			if err != nil {
				return nil, err
			}
			if len(resp.Data) > 0 {
				result[key] = resp.Data[0]
			}
		}
	}
	if keyErrs != nil {
		return result, keyErrs
	}
	return result, nil
}

const (
	lazyMaterializationSetting = "\nSETTINGS query_plan_max_limit_for_lazy_materialization = 1000"
)

// Codes of errors for a setting the server does not know or does not let this user change.
var chSettingErrors = []int32{
	115, // UNKNOWN_SETTING
	164, // READONLY
	452, // SETTING_CONSTRAINT_VIOLATION
}

// queryLazy raises the lazy materialization limit so that the payload is read
// for the returned rows only. Servers that reject the setting run the query
// without it, reading more.
func (s *logStoreImpl) queryLazy(ctx context.Context, query string, args []any) (clickhouse.Rows, error) {
	if !s.noLazySetting.Load() {
		rows, err := s.chDB.Query(ctx, query+lazyMaterializationSetting, args...)
		var ex *chgo.Exception
		if err == nil || !errors.As(err, &ex) || !slices.Contains(chSettingErrors, ex.Code) {
			return rows, err
		}
		s.noLazySetting.Store(true)
	}
	return s.chDB.Query(ctx, query, args...)
}

func newerAttempt(a, b *models.Attempt) bool {
	if !a.Time.Equal(b.Time) {
		return a.Time.After(b.Time)
	}
	return a.ID > b.ID
}

package pglogstore

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

// ListLatestAttempts probes the (event_id, time) index once per key.
func (s *logStore) ListLatestAttempts(ctx context.Context, lookups []driver.LatestAttemptLookup) (map[driver.AttemptKey]*driver.AttemptRecord, error) {
	if len(lookups) == 0 {
		return map[driver.AttemptKey]*driver.AttemptRecord{}, nil
	}
	tenants := make([]string, len(lookups))
	events := make([]string, len(lookups))
	destinations := make([]string, len(lookups))
	bounds := make([]time.Time, len(lookups))
	for i, l := range lookups {
		tenants[i], events[i], destinations[i], bounds[i] = l.Key.TenantID, l.Key.EventID, l.Key.DestinationID, l.NotBefore
	}

	rows, err := s.db.Query(ctx, `
		SELECT
			l.id, l.event_id, l.tenant_id, l.destination_id, l.destination_type, l.topic,
			l.status, l.time, l.attempt_number, l.manual, l.code, l.response_data,
			l.event_time, l.eligible_for_retry, l.event_data, l.event_metadata, l.latency_ms
		FROM unnest($1::text[], $2::text[], $3::text[], $4::timestamptz[]) AS k(t, e, d, nb)
		CROSS JOIN LATERAL (
			SELECT * FROM attempts
			WHERE tenant_id = k.t AND event_id = k.e AND destination_id = k.d AND time >= k.nb
			ORDER BY time DESC, id DESC
			LIMIT 1
		) l
	`, tenants, events, destinations, bounds)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()
	records, err := scanAttemptRecords(rows)
	if err != nil {
		return nil, err
	}

	result := make(map[driver.AttemptKey]*driver.AttemptRecord, len(records))
	for _, r := range records {
		key := driver.AttemptKey{TenantID: r.Attempt.TenantID, EventID: r.Attempt.EventID, DestinationID: r.Attempt.DestinationID}
		result[key] = r.AttemptRecord
	}
	return result, nil
}

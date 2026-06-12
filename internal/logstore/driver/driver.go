package driver

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

// TimeFilter represents time-based filter criteria with support for
// both inclusive (GTE/LTE) and exclusive (GT/LT) comparisons.
type TimeFilter struct {
	GTE *time.Time // Greater than or equal (>=)
	LTE *time.Time // Less than or equal (<=)
	GT  *time.Time // Greater than (>)
	LT  *time.Time // Less than (<)
}

type Records interface {
	ListEvent(context.Context, ListEventRequest) (ListEventResponse, error)
	ListAttempt(context.Context, ListAttemptRequest) (ListAttemptResponse, error)
	RetrieveEvent(ctx context.Context, request RetrieveEventRequest) (*models.Event, error)
	RetrieveAttempt(ctx context.Context, request RetrieveAttemptRequest) (*AttemptRecord, error)
	InsertMany(context.Context, []*models.LogEntry) error
}

// LogStore is the combined interface that all driver implementations must satisfy.
type LogStore interface {
	Records
	Metrics
}

type ListEventRequest struct {
	Next           string
	Prev           string
	Limit          int
	TimeFilter     TimeFilter // optional - filter events by time
	TenantIDs      []string   // optional - filter by tenant (if empty, returns all tenants)
	EventIDs       []string   // optional - filter by event ID
	DestinationIDs []string   // optional
	Topics         []string   // optional
	SortOrder      string     // optional: "asc", "desc" (default: "desc")
}

type ListEventResponse struct {
	Data []*models.Event
	Next string
	Prev string
}

type ListAttemptRequest struct {
	Next             string
	Prev             string
	Limit            int
	TimeFilter       TimeFilter // optional - filter attempts by time
	TenantIDs        []string   // optional - filter by tenant (if empty, returns all tenants)
	EventIDs         []string   // optional - filter by event ID
	DestinationIDs   []string   // optional
	DestinationTypes []string   // optional - filter by destination type
	Status           string     // optional: "success", "failed"
	Topics           []string   // optional
	SortOrder        string     // optional: "asc", "desc" (default: "desc")
}

type ListAttemptResponse struct {
	Data []*AttemptRecord
	Next string
	Prev string
}

type RetrieveEventRequest struct {
	TenantID string // optional - filter by tenant (if empty, searches all tenants)
	EventID  string // required
}

type RetrieveAttemptRequest struct {
	TenantID  string // optional - filter by tenant (if empty, searches all tenants)
	AttemptID string // required
}

// AttemptRecord represents an attempt query result with optional Event population.
type AttemptRecord struct {
	Attempt *models.Attempt
	Event   *models.Event // optionally populated for query results
}

// DedupeEntriesByAttemptID collapses intra-batch duplicates (same Attempt.ID)
// to a single entry. Duplicates arise from MQ redelivery and producer
// re-publish, so they can carry different MQ message IDs; copies are
// byte-identical, so the last occurrence wins at the first occurrence's
// position. InsertMany implementations must tolerate intra-batch duplicates
// regardless of caller — e.g. PostgreSQL rejects the same conflict key twice
// in one INSERT ... ON CONFLICT DO UPDATE statement (SQLSTATE 21000).
func DedupeEntriesByAttemptID(entries []*models.LogEntry) []*models.LogEntry {
	deduped := make([]*models.LogEntry, 0, len(entries))
	index := make(map[string]int, len(entries))
	for _, entry := range entries {
		if i, ok := index[entry.Attempt.ID]; ok {
			deduped[i] = entry
			continue
		}
		index[entry.Attempt.ID] = len(deduped)
		deduped = append(deduped, entry)
	}
	return deduped
}

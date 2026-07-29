package eventlog

import (
	"fmt"
	"testing"
	"time"
)

func newTestLog(cap int) *Log {
	return New(cap, time.Hour)
}

func TestAddAndQuery(t *testing.T) {
	log := newTestLog(100)

	now := time.Now()
	for i := 0; i < 10; i++ {
		log.Add(Record{
			EventID:     fmt.Sprintf("evt-%d", i),
			GroupName:   "test",
			TenantID:    "t-0",
			Status:      StatusPublished,
			PublishedAt: now.Add(time.Duration(i) * time.Second),
		})
	}

	r := log.Query(QueryParams{})
	if r.Total != 10 {
		t.Fatalf("expected 10 total, got %d", r.Total)
	}
	if r.Events[0].EventID != "evt-9" {
		t.Fatalf("expected newest first, got %s", r.Events[0].EventID)
	}

	r = log.Query(QueryParams{Oldest: true})
	if r.Events[0].EventID != "evt-0" {
		t.Fatalf("expected oldest first, got %s", r.Events[0].EventID)
	}
}

func TestFilterByStatus(t *testing.T) {
	log := newTestLog(100)
	now := time.Now()

	log.Add(Record{EventID: "e1", Status: StatusPublished, PublishedAt: now})
	log.Add(Record{EventID: "e2", Status: StatusError, Error: "status 500", PublishedAt: now})
	log.Add(Record{EventID: "e3", Status: StatusDelivered, PublishedAt: now})
	log.Add(Record{EventID: "e4", Status: StatusMissing, PublishedAt: now})
	log.Add(Record{EventID: "e5", Status: StatusError, Error: "timeout", PublishedAt: now})

	r := log.Query(QueryParams{Statuses: []Status{StatusError}})
	if r.Total != 2 {
		t.Fatalf("expected 2 errors, got %d", r.Total)
	}

	r = log.Query(QueryParams{Statuses: []Status{StatusError, StatusMissing}})
	if r.Total != 3 {
		t.Fatalf("expected 3 error+missing, got %d", r.Total)
	}

	counts := log.Counts()
	if counts[StatusError] != 2 || counts[StatusMissing] != 1 || counts[StatusPublished] != 1 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
}

func TestUpdateMovesBuckets(t *testing.T) {
	log := newTestLog(100)
	now := time.Now()

	log.Add(Record{EventID: "e1", Status: StatusPublished, PublishLatency: 120, PublishedAt: now})

	delivered := now.Add(50 * time.Millisecond)
	ok := log.Update("e1", func(r *Record) {
		r.Status = StatusDelivered
		r.DeliveredAt = &delivered
		r.E2ELatency = 350
	})
	if !ok {
		t.Fatal("update should return true")
	}

	// Should have moved out of published bucket
	r := log.Query(QueryParams{Statuses: []Status{StatusPublished}})
	if r.Total != 0 {
		t.Fatalf("expected 0 published after move, got %d", r.Total)
	}

	r = log.Query(QueryParams{Statuses: []Status{StatusDelivered}})
	if r.Total != 1 {
		t.Fatalf("expected 1 delivered, got %d", r.Total)
	}
	if r.Events[0].E2ELatency != 350 {
		t.Fatalf("expected e2e 350, got %d", r.Events[0].E2ELatency)
	}

	ok = log.Update("nonexistent", func(r *Record) { r.Status = StatusError })
	if ok {
		t.Fatal("update of nonexistent should return false")
	}
}

func TestRingBufferEvictionPerStatus(t *testing.T) {
	log := newTestLog(5)
	now := time.Now()

	for i := 0; i < 8; i++ {
		log.Add(Record{
			EventID:     fmt.Sprintf("evt-%d", i),
			Status:      StatusPublished,
			PublishedAt: now.Add(time.Duration(i) * time.Millisecond),
		})
	}

	r := log.Query(QueryParams{Statuses: []Status{StatusPublished}, Oldest: true})
	if r.Total != 5 {
		t.Fatalf("expected 5 (capped), got %d", r.Total)
	}
	if r.Events[0].EventID != "evt-3" {
		t.Fatalf("expected evt-3 as oldest, got %s", r.Events[0].EventID)
	}

	ok := log.Update("evt-0", func(r *Record) {})
	if ok {
		t.Fatal("evicted event should not be updatable")
	}
}

func TestTTLExpiration(t *testing.T) {
	log := New(100, 10*time.Millisecond)
	now := time.Now()
	log.now = func() time.Time { return now }

	log.Add(Record{EventID: "old", Status: StatusDelivered, PublishedAt: now.Add(-time.Hour)})
	log.Add(Record{EventID: "fresh", Status: StatusDelivered, PublishedAt: now})

	r := log.Query(QueryParams{})
	if r.Total != 1 {
		t.Fatalf("expected 1 (fresh only), got %d", r.Total)
	}
	if r.Events[0].EventID != "fresh" {
		t.Fatalf("expected 'fresh', got %s", r.Events[0].EventID)
	}
}

func TestPagination(t *testing.T) {
	log := newTestLog(100)
	now := time.Now()

	for i := 0; i < 25; i++ {
		log.Add(Record{
			EventID:     fmt.Sprintf("evt-%d", i),
			Status:      StatusPublished,
			PublishedAt: now.Add(time.Duration(i) * time.Millisecond),
		})
	}

	r := log.Query(QueryParams{Limit: 10, Page: 1})
	if len(r.Events) != 10 || !r.HasMore || r.Total != 25 {
		t.Fatalf("page 1: len=%d has_more=%v total=%d", len(r.Events), r.HasMore, r.Total)
	}

	r = log.Query(QueryParams{Limit: 10, Page: 3})
	if len(r.Events) != 5 || r.HasMore {
		t.Fatalf("page 3: len=%d has_more=%v", len(r.Events), r.HasMore)
	}
}

func TestReset(t *testing.T) {
	log := newTestLog(100)
	log.Add(Record{EventID: "e1", Status: StatusPublished, PublishedAt: time.Now()})
	log.Reset()

	r := log.Query(QueryParams{})
	if r.Total != 0 {
		t.Fatalf("expected 0 after reset, got %d", r.Total)
	}
	// After reset, can re-add same eventID
	log.Add(Record{EventID: "e1", Status: StatusPublished, PublishedAt: time.Now()})
	r = log.Query(QueryParams{})
	if r.Total != 1 {
		t.Fatalf("expected 1 after re-add, got %d", r.Total)
	}
}

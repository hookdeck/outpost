package eventlog

import (
	"fmt"
	"testing"
	"time"
)

func TestAddAndQuery(t *testing.T) {
	log := New(100)

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

	// Query all
	r := log.Query(QueryParams{})
	if r.Total != 10 {
		t.Fatalf("expected 10 total, got %d", r.Total)
	}
	// Default newest first
	if r.Events[0].EventID != "evt-9" {
		t.Fatalf("expected newest first, got %s", r.Events[0].EventID)
	}

	// Query oldest
	r = log.Query(QueryParams{Oldest: true})
	if r.Events[0].EventID != "evt-0" {
		t.Fatalf("expected oldest first, got %s", r.Events[0].EventID)
	}
}

func TestFilterByStatus(t *testing.T) {
	log := New(100)

	log.Add(Record{EventID: "e1", Status: StatusPublished})
	log.Add(Record{EventID: "e2", Status: StatusError, Error: "status 500"})
	log.Add(Record{EventID: "e3", Status: StatusDelivered})
	log.Add(Record{EventID: "e4", Status: StatusMissing})
	log.Add(Record{EventID: "e5", Status: StatusError, Error: "timeout"})

	r := log.Query(QueryParams{Statuses: []Status{StatusError}})
	if r.Total != 2 {
		t.Fatalf("expected 2 errors, got %d", r.Total)
	}

	r = log.Query(QueryParams{Statuses: []Status{StatusError, StatusMissing}})
	if r.Total != 3 {
		t.Fatalf("expected 3 error+missing, got %d", r.Total)
	}
}

func TestUpdate(t *testing.T) {
	log := New(100)

	log.Add(Record{EventID: "e1", Status: StatusPublished, PublishLatency: 120})

	delivered := time.Now()
	ok := log.Update("e1", func(r *Record) {
		r.Status = StatusDelivered
		r.DeliveredAt = &delivered
		r.E2ELatency = 350
	})
	if !ok {
		t.Fatal("update should return true")
	}

	r := log.Query(QueryParams{Statuses: []Status{StatusDelivered}})
	if r.Total != 1 {
		t.Fatalf("expected 1 delivered, got %d", r.Total)
	}
	if r.Events[0].E2ELatency != 350 {
		t.Fatalf("expected e2e 350, got %d", r.Events[0].E2ELatency)
	}

	// Update nonexistent
	ok = log.Update("nonexistent", func(r *Record) {
		r.Status = StatusError
	})
	if ok {
		t.Fatal("update of nonexistent should return false")
	}
}

func TestRingBufferEviction(t *testing.T) {
	log := New(5)

	for i := 0; i < 8; i++ {
		log.Add(Record{EventID: fmt.Sprintf("evt-%d", i), Status: StatusPublished})
	}

	r := log.Query(QueryParams{Oldest: true})
	if r.Total != 5 {
		t.Fatalf("expected 5 (capped), got %d", r.Total)
	}
	// Oldest should be evt-3 (0,1,2 evicted)
	if r.Events[0].EventID != "evt-3" {
		t.Fatalf("expected evt-3 as oldest, got %s", r.Events[0].EventID)
	}

	// Evicted event should not be updatable
	ok := log.Update("evt-0", func(r *Record) {})
	if ok {
		t.Fatal("evicted event should not be updatable")
	}
}

func TestPagination(t *testing.T) {
	log := New(100)
	for i := 0; i < 25; i++ {
		log.Add(Record{EventID: fmt.Sprintf("evt-%d", i), Status: StatusPublished})
	}

	r := log.Query(QueryParams{Limit: 10, Page: 1})
	if len(r.Events) != 10 {
		t.Fatalf("expected 10 events on page 1, got %d", len(r.Events))
	}
	if !r.HasMore {
		t.Fatal("expected has_more=true")
	}
	if r.Total != 25 {
		t.Fatalf("expected total 25, got %d", r.Total)
	}

	r = log.Query(QueryParams{Limit: 10, Page: 3})
	if len(r.Events) != 5 {
		t.Fatalf("expected 5 events on page 3, got %d", len(r.Events))
	}
	if r.HasMore {
		t.Fatal("expected has_more=false on last page")
	}
}

func TestReset(t *testing.T) {
	log := New(100)
	log.Add(Record{EventID: "e1", Status: StatusPublished})
	log.Reset()

	r := log.Query(QueryParams{})
	if r.Total != 0 {
		t.Fatalf("expected 0 after reset, got %d", r.Total)
	}
}

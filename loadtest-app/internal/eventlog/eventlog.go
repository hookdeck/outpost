package eventlog

import (
	"slices"
	"sync"
	"time"
)

type Status string

const (
	StatusPublished Status = "published"
	StatusDelivered Status = "delivered"
	StatusMissing   Status = "missing"
	StatusError     Status = "error"
	StatusRecovered Status = "recovered"
)

type Record struct {
	EventID        string        `json:"event_id"`
	GroupName      string        `json:"group_name"`
	TenantID       string        `json:"tenant_id"`
	Topic          string        `json:"topic"`
	Status         Status        `json:"status"`
	PublishedAt    time.Time     `json:"published_at"`
	PublishLatency int64         `json:"publish_latency_ms,omitempty"`
	DeliveredAt    *time.Time    `json:"delivered_at,omitempty"`
	E2ELatency     int64         `json:"e2e_latency_ms,omitempty"`
	Error          string        `json:"error,omitempty"`
	StatusCode     int           `json:"status_code,omitempty"`
}

type QueryParams struct {
	Statuses []Status
	Group    string
	Page     int
	Limit    int
	Oldest   bool // false = newest first (default)
}

type QueryResult struct {
	Events  []Record `json:"events"`
	Total   int      `json:"total"`
	Page    int      `json:"page"`
	Limit   int      `json:"limit"`
	HasMore bool     `json:"has_more"`
}

// Log is a bounded, in-memory event log with O(1) updates by eventID.
type Log struct {
	mu      sync.RWMutex
	records []Record       // ring buffer
	index   map[string]int // eventID → position in records
	head    int            // next write position
	size    int            // current number of records
	cap     int            // max capacity
}

func New(capacity int) *Log {
	return &Log{
		records: make([]Record, capacity),
		index:   make(map[string]int, capacity),
		cap:     capacity,
	}
}

// Add inserts a new record. If the buffer is full, the oldest record is evicted.
func (l *Log) Add(r Record) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Evict old entry from index if overwriting
	if l.size == l.cap {
		old := l.records[l.head]
		delete(l.index, old.EventID)
	}

	l.records[l.head] = r
	l.index[r.EventID] = l.head
	l.head = (l.head + 1) % l.cap
	if l.size < l.cap {
		l.size++
	}
}

// Update modifies an existing record by eventID. Returns false if not found.
func (l *Log) Update(eventID string, fn func(*Record)) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	pos, ok := l.index[eventID]
	if !ok {
		return false
	}
	fn(&l.records[pos])
	return true
}

// Query returns a paginated, filtered view of the log.
func (l *Log) Query(p QueryParams) QueryResult {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Page <= 0 {
		p.Page = 1
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	// Collect matching records in insertion order (oldest first)
	var matched []Record
	for i := 0; i < l.size; i++ {
		idx := (l.head - l.size + i + l.cap) % l.cap
		r := l.records[idx]
		if l.matchesFilter(r, p) {
			matched = append(matched, r)
		}
	}

	total := len(matched)

	// Reverse for newest-first (default)
	if !p.Oldest {
		for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
			matched[i], matched[j] = matched[j], matched[i]
		}
	}

	// Paginate
	start := (p.Page - 1) * p.Limit
	if start >= len(matched) {
		return QueryResult{
			Events:  []Record{},
			Total:   total,
			Page:    p.Page,
			Limit:   p.Limit,
			HasMore: false,
		}
	}
	end := min(start+p.Limit, len(matched))

	return QueryResult{
		Events:  matched[start:end],
		Total:   total,
		Page:    p.Page,
		Limit:   p.Limit,
		HasMore: end < len(matched),
	}
}

func (l *Log) matchesFilter(r Record, p QueryParams) bool {
	if len(p.Statuses) > 0 && !slices.Contains(p.Statuses, r.Status) {
		return false
	}
	if p.Group != "" && r.GroupName != p.Group {
		return false
	}
	return true
}

// Reset clears the log.
func (l *Log) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.index = make(map[string]int, l.cap)
	l.head = 0
	l.size = 0
}

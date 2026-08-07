package eventlog

import (
	"sort"
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

var allStatuses = []Status{StatusPublished, StatusDelivered, StatusMissing, StatusError, StatusRecovered}

const (
	DefaultPerStatusCapacity = 100_000
	DefaultTTL               = time.Hour
)

type Record struct {
	EventID        string     `json:"event_id"`
	GroupName      string     `json:"group_name"`
	TenantID       string     `json:"tenant_id"`
	Topic          string     `json:"topic"`
	Status         Status     `json:"status"`
	PublishedAt    time.Time  `json:"published_at"` // t0 — scheduled send
	PublishLatency int64      `json:"publish_latency_ms,omitempty"`
	WireLatency    int64      `json:"wire_latency_ms,omitempty"` // request write → ack, excludes generator lag
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	E2ELatency     int64      `json:"e2e_latency_ms,omitempty"`
	Error          string     `json:"error,omitempty"`
	StatusCode     int        `json:"status_code,omitempty"`
}

type QueryParams struct {
	Statuses []Status
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

// bucket is a fixed-capacity ring buffer of records.
// Tombstoned slots have EventID == "".
type bucket struct {
	records []Record
	head    int // next write position
	size    int // count of live + tombstoned slots filled (i.e. valid window length)
}

func newBucket(cap int) *bucket {
	return &bucket{records: make([]Record, cap)}
}

// position records where an eventID lives.
type position struct {
	status Status
	idx    int
}

// Log holds per-status ring buffers with TTL-based and capacity-based eviction.
type Log struct {
	mu      sync.RWMutex
	buckets map[Status]*bucket
	index   map[string]position
	cap     int
	ttl     time.Duration
	now     func() time.Time // overridable for tests
}

func New(perStatusCap int, ttl time.Duration) *Log {
	if perStatusCap <= 0 {
		perStatusCap = DefaultPerStatusCapacity
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	buckets := make(map[Status]*bucket, len(allStatuses))
	for _, s := range allStatuses {
		buckets[s] = newBucket(perStatusCap)
	}
	return &Log{
		buckets: buckets,
		index:   make(map[string]position),
		cap:     perStatusCap,
		ttl:     ttl,
		now:     time.Now,
	}
}

func NewDefault() *Log { return New(DefaultPerStatusCapacity, DefaultTTL) }

// Add inserts a new record. If the buffer for the status is full, the oldest slot is overwritten.
func (l *Log) Add(r Record) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.insert(r)
}

// must hold l.mu.
func (l *Log) insert(r Record) {
	b := l.buckets[r.Status]
	if b == nil {
		return
	}
	// Evict whatever currently lives at head
	if b.size == l.cap {
		old := b.records[b.head]
		if old.EventID != "" {
			delete(l.index, old.EventID)
		}
	}
	b.records[b.head] = r
	l.index[r.EventID] = position{status: r.Status, idx: b.head}
	b.head = (b.head + 1) % l.cap
	if b.size < l.cap {
		b.size++
	}
}

// Update modifies an existing record by eventID. If the mutator changes Status, the record
// is tombstoned in the old bucket and re-inserted into the new bucket.
// Returns false if eventID is not present.
func (l *Log) Update(eventID string, fn func(*Record)) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	pos, ok := l.index[eventID]
	if !ok {
		return false
	}
	b := l.buckets[pos.status]
	cur := b.records[pos.idx]
	if cur.EventID != eventID {
		// stale index entry; defensive
		delete(l.index, eventID)
		return false
	}
	fn(&cur)
	if cur.Status == pos.status {
		b.records[pos.idx] = cur
		return true
	}
	// Status changed — tombstone old slot and reinsert into new bucket
	b.records[pos.idx] = Record{}
	delete(l.index, eventID)
	l.insert(cur)
	return true
}

// Query returns a paginated, filtered view across the requested status buckets.
// Records are returned newest-first by PublishedAt unless Oldest is true.
// Records older than TTL are filtered out.
func (l *Log) Query(p QueryParams) QueryResult {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Page <= 0 {
		p.Page = 1
	}

	l.mu.RLock()
	defer l.mu.RUnlock()

	statuses := p.Statuses
	if len(statuses) == 0 {
		statuses = allStatuses
	}

	cutoff := l.now().Add(-l.ttl)
	var matched []Record
	for _, s := range statuses {
		b := l.buckets[s]
		if b == nil {
			continue
		}
		for i := 0; i < b.size; i++ {
			idx := (b.head - b.size + i + l.cap) % l.cap
			r := b.records[idx]
			if r.EventID == "" {
				continue
			}
			if r.PublishedAt.Before(cutoff) {
				continue
			}
			matched = append(matched, r)
		}
	}

	if p.Oldest {
		sort.Slice(matched, func(i, j int) bool { return matched[i].PublishedAt.Before(matched[j].PublishedAt) })
	} else {
		sort.Slice(matched, func(i, j int) bool { return matched[i].PublishedAt.After(matched[j].PublishedAt) })
	}

	total := len(matched)
	start := (p.Page - 1) * p.Limit
	if start >= total {
		return QueryResult{Events: []Record{}, Total: total, Page: p.Page, Limit: p.Limit, HasMore: false}
	}
	end := min(start+p.Limit, total)

	return QueryResult{
		Events:  matched[start:end],
		Total:   total,
		Page:    p.Page,
		Limit:   p.Limit,
		HasMore: end < total,
	}
}

// Counts returns the live record count per status.
func (l *Log) Counts() map[Status]int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[Status]int, len(allStatuses))
	cutoff := l.now().Add(-l.ttl)
	for s, b := range l.buckets {
		c := 0
		for i := 0; i < b.size; i++ {
			idx := (b.head - b.size + i + l.cap) % l.cap
			r := b.records[idx]
			if r.EventID == "" || r.PublishedAt.Before(cutoff) {
				continue
			}
			c++
		}
		out[s] = c
	}
	return out
}

// Reset clears every bucket.
func (l *Log) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.index = make(map[string]position)
	for _, b := range l.buckets {
		for i := range b.records {
			b.records[i] = Record{}
		}
		b.head = 0
		b.size = 0
	}
}


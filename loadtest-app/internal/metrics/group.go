package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// GroupMetrics tracks all metrics for a single group.
type GroupMetrics struct {
	mu sync.RWMutex

	publishTotal  atomic.Int64
	publishErrors atomic.Int64
	deliveryTotal atomic.Int64
	duplicates    atomic.Int64
	missingTotal  atomic.Int64

	publishLatency *Histogram // time for Outpost to respond to publish
	e2eLatency     *Histogram // time from publish to webhook delivery

	publishRate  *RateCounter
	deliveryRate *RateCounter
}

func NewGroupMetrics() *GroupMetrics {
	return &GroupMetrics{
		publishLatency: NewHistogram(1, 60000, 3),    // 1ms - 60s, 3 sig figs
		e2eLatency:     NewHistogram(1, 120000, 3),   // 1ms - 120s
		publishRate:    NewRateCounter(10*time.Second, 10),
		deliveryRate:   NewRateCounter(10*time.Second, 10),
	}
}

func (g *GroupMetrics) RecordPublish(latency time.Duration) {
	g.publishTotal.Add(1)
	g.publishLatency.Record(latency.Milliseconds())
	g.publishRate.Record(time.Now())
}

func (g *GroupMetrics) RecordPublishError() {
	g.publishTotal.Add(1)
	g.publishErrors.Add(1)
	g.publishRate.Record(time.Now())
}

func (g *GroupMetrics) RecordDelivery(e2eLatency time.Duration) {
	g.deliveryTotal.Add(1)
	g.e2eLatency.Record(e2eLatency.Milliseconds())
	g.deliveryRate.Record(time.Now())
}

func (g *GroupMetrics) RecordDuplicate() {
	g.duplicates.Add(1)
}

func (g *GroupMetrics) RecordMissing() {
	g.missingTotal.Add(1)
}

func (g *GroupMetrics) Reset() {
	g.publishTotal.Store(0)
	g.publishErrors.Store(0)
	g.deliveryTotal.Store(0)
	g.duplicates.Store(0)
	g.missingTotal.Store(0)
	g.publishLatency.Reset()
	g.e2eLatency.Reset()
	g.publishRate.Reset()
	g.deliveryRate.Reset()
}

func (g *GroupMetrics) Snapshot() GroupSnapshot {
	now := time.Now()
	return GroupSnapshot{
		PublishTotal:     g.publishTotal.Load(),
		PublishErrors:    g.publishErrors.Load(),
		DeliveryTotal:    g.deliveryTotal.Load(),
		Duplicates:       g.duplicates.Load(),
		MissingTotal:     g.missingTotal.Load(),
		PublishRatePerSec: g.publishRate.Rate(now),
		DeliveryRatePerSec: g.deliveryRate.Rate(now),
		PublishLatencyP50: g.publishLatency.Percentile(50),
		PublishLatencyP95: g.publishLatency.Percentile(95),
		PublishLatencyP99: g.publishLatency.Percentile(99),
		E2ELatencyP50:    g.e2eLatency.Percentile(50),
		E2ELatencyP95:    g.e2eLatency.Percentile(95),
		E2ELatencyP99:    g.e2eLatency.Percentile(99),
	}
}

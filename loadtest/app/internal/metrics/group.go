package metrics

import (
	"sync/atomic"
	"time"
)

// GroupMetrics tracks run-cumulative metrics for a single group, for the
// live dashboard. Time-bucketed distributions go to Prometheus; exact
// per-phase accounting goes to the run ledger.
type GroupMetrics struct {
	publishTotal  atomic.Int64
	publishErrors atomic.Int64
	deliveryTotal atomic.Int64
	duplicates    atomic.Int64
	missingTotal  atomic.Int64
	cutoffTotal   atomic.Int64

	publishLatency *Histogram // scheduled send → Publish API ack
	e2eLatency     *Histogram // scheduled send → first byte at destination
	generatorLag   *Histogram // scheduled send → request actually written

	publishRate  *RateCounter
	deliveryRate *RateCounter
}

func NewGroupMetrics() *GroupMetrics {
	return &GroupMetrics{
		publishLatency: NewLatencyHistogram(),
		e2eLatency:     NewLatencyHistogram(),
		generatorLag:   NewLatencyHistogram(),
		publishRate:    NewRateCounter(10*time.Second, 10),
		deliveryRate:   NewRateCounter(10*time.Second, 10),
	}
}

func (g *GroupMetrics) RecordPublish(latency time.Duration) {
	g.publishTotal.Add(1)
	g.publishLatency.Record(latency)
	g.publishRate.Record(time.Now())
}

func (g *GroupMetrics) RecordPublishError() {
	g.publishTotal.Add(1)
	g.publishErrors.Add(1)
	g.publishRate.Record(time.Now())
}

// RecordGeneratorLag records how far behind schedule the generator was when
// it actually wrote the request. Sustained non-zero lag means the generator
// is the thing being measured, which voids a run.
func (g *GroupMetrics) RecordGeneratorLag(lag time.Duration) {
	if lag < 0 {
		lag = 0
	}
	g.generatorLag.Record(lag)
}

func (g *GroupMetrics) RecordDelivery(e2eLatency time.Duration) {
	g.deliveryTotal.Add(1)
	g.e2eLatency.Record(e2eLatency)
	g.deliveryRate.Record(time.Now())
}

func (g *GroupMetrics) RecordDuplicate() { g.duplicates.Add(1) }
func (g *GroupMetrics) RecordMissing()   { g.missingTotal.Add(1) }
func (g *GroupMetrics) RecordCutoff()    { g.cutoffTotal.Add(1) }

// RecordRecovered is a delivery that beat the sweep only by arriving late. The
// dashboard must see it as a delivery like any other, or a backlogged run reads
// as zero throughput at zero latency.
func (g *GroupMetrics) RecordRecovered(e2eLatency time.Duration) {
	g.missingTotal.Add(-1)
	g.RecordDelivery(e2eLatency)
}

func (g *GroupMetrics) Reset() {
	g.publishTotal.Store(0)
	g.publishErrors.Store(0)
	g.deliveryTotal.Store(0)
	g.duplicates.Store(0)
	g.missingTotal.Store(0)
	g.cutoffTotal.Store(0)
	g.publishLatency.Reset()
	g.e2eLatency.Reset()
	g.generatorLag.Reset()
	g.publishRate.Reset()
	g.deliveryRate.Reset()
}

func (g *GroupMetrics) Snapshot() GroupSnapshot {
	now := time.Now()
	return GroupSnapshot{
		PublishTotal:       g.publishTotal.Load(),
		PublishErrors:      g.publishErrors.Load(),
		DeliveryTotal:      g.deliveryTotal.Load(),
		Duplicates:         g.duplicates.Load(),
		MissingTotal:       g.missingTotal.Load(),
		CutoffTotal:        g.cutoffTotal.Load(),
		PublishRatePerSec:  g.publishRate.Rate(now),
		DeliveryRatePerSec: g.deliveryRate.Rate(now),
		PublishLatencyP50:  g.publishLatency.Percentile(50),
		PublishLatencyP90:  g.publishLatency.Percentile(90),
		PublishLatencyP95:  g.publishLatency.Percentile(95),
		PublishLatencyP99:  g.publishLatency.Percentile(99),
		PublishLatencyMax:  g.publishLatency.Max(),
		E2ELatencyP50:      g.e2eLatency.Percentile(50),
		E2ELatencyP90:      g.e2eLatency.Percentile(90),
		E2ELatencyP95:      g.e2eLatency.Percentile(95),
		E2ELatencyP99:      g.e2eLatency.Percentile(99),
		E2ELatencyMax:      g.e2eLatency.Max(),
		GeneratorLagP50:    g.generatorLag.Percentile(50),
		GeneratorLagP99:    g.generatorLag.Percentile(99),
	}
}

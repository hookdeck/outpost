package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Prometheus instruments. Every series is labelled with the run it belongs to,
// the profile that produced it, and the phase it was published in, so a query
// can isolate the steady-state window of one run without any post-hoc
// filtering by timestamp.
//
// Latency distributions are native histograms: mergeable across scrapes and
// high enough resolution to publish a p99. Classic buckets interpolate, and
// the interpolation error at p99 is larger than the differences these runs
// are trying to show.
var (
	labels = []string{"run_id", "profile", "phase"}

	nativeHistogram = func(name, help string) *prometheus.HistogramVec {
		return prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:                            name,
			Help:                            help,
			NativeHistogramBucketFactor:     1.02,
			NativeHistogramMaxBucketNumber:  200,
			NativeHistogramMinResetDuration: time.Hour,
			// Classic buckets kept as a fallback for a Prometheus that has
			// native histograms disabled; the export prefers native.
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 20),
		}, labels)
	}

	PublishLatency = nativeHistogram(
		"loadtest_publish_latency_seconds",
		"Scheduled send (t0) to Publish API ack.")
	DeliveryLatency = nativeHistogram(
		"loadtest_delivery_latency_seconds",
		"Scheduled send (t0) to first byte at the destination.")
	GeneratorLag = nativeHistogram(
		"loadtest_generator_lag_seconds",
		"Scheduled send (t0) to the request actually being written. Sustained lag voids a run.")

	PublishedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loadtest_published_total",
		Help: "Events accepted by the Publish API.",
	}, labels)
	PublishErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loadtest_publish_errors_total",
		Help: "Publish attempts that did not return 2xx.",
	}, labels)
	DeliveredTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loadtest_delivered_total",
		Help: "Individual destination deliveries received.",
	}, labels)
	EventsCompletedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loadtest_events_completed_total",
		Help: "Events for which every expected destination delivered.",
	}, labels)
	MissingTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loadtest_missing_total",
		Help: "Events swept as undelivered past the grace period.",
	}, labels)
	RecoveredTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loadtest_recovered_total",
		Help: "Events that arrived after being swept as missing.",
	}, labels)
	DuplicatesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loadtest_duplicates_total",
		Help: "Deliveries for an event that had already completed.",
	}, labels)
	CutoffTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loadtest_cutoff_total",
		Help: "Events still in flight when the run ended. Not failures.",
	}, labels)
	BytesPublishedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "loadtest_bytes_published_total",
		Help: "Payload bytes offered to the Publish API.",
	}, labels)

	profileLabels = []string{"run_id", "profile"}

	InFlight = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadtest_in_flight",
		Help: "Events published but not yet fully delivered.",
	}, profileLabels)
	OfferedRate = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadtest_offered_rate",
		Help: "Events per second the generator is scheduled to publish.",
	}, profileLabels)

	// RunInfo is 1 while a run is active, carrying its identity as labels so a
	// dashboard or export can resolve a run_id to what it actually was.
	RunInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadtest_run_info",
		Help: "Active run identity.",
	}, []string{"run_id", "name", "target", "phase"})

	RunPhaseStart = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "loadtest_run_phase_start_seconds",
		Help: "Unix time at which each phase of the run began.",
	}, []string{"run_id", "phase"})
)

// Registry holds every loadtest instrument. It is deliberately not the default
// registry: the /metrics endpoint should expose this app's own view, plus Go
// runtime, and nothing accidentally pulled in by a dependency.
var Registry = prometheus.NewRegistry()

// Emit is the single write path for run measurements. It updates Prometheus
// and the in-process ledger together, so the two can never drift and no call
// site has to remember to do both.
//
// The split of duties: Prometheus owns distributions and time series, the
// ledger owns exact counts. A ledger that depended on scrapes landing would
// turn a monitoring hiccup into an accounting hole.
type Emit struct {
	RunID   string
	Profile string
	Phase   string
}

func (e Emit) labels() prometheus.Labels {
	return prometheus.Labels{"run_id": e.RunID, "profile": e.Profile, "phase": e.Phase}
}

func (e Emit) Published(latency time.Duration, payloadBytes int) {
	l := e.labels()
	PublishLatency.With(l).Observe(nonNeg(latency))
	PublishedTotal.With(l).Inc()
	BytesPublishedTotal.With(l).Add(float64(payloadBytes))
	EventLedger.add(e, func(c *Counts) {
		c.Published++
		c.PayloadBytes += int64(payloadBytes)
	})
}

func (e Emit) PublishError() {
	PublishErrorsTotal.With(e.labels()).Inc()
	EventLedger.add(e, func(c *Counts) { c.PublishErrors++ })
}

func (e Emit) GeneratorLagged(lag time.Duration) {
	GeneratorLag.With(e.labels()).Observe(nonNeg(lag))
}

func (e Emit) Delivered(latency time.Duration, eventComplete bool) {
	l := e.labels()
	DeliveryLatency.With(l).Observe(nonNeg(latency))
	DeliveredTotal.With(l).Inc()
	if eventComplete {
		EventsCompletedTotal.With(l).Inc()
	}
	EventLedger.add(e, func(c *Counts) {
		c.Deliveries++
		if eventComplete {
			c.Completed++
		}
	})
}

func (e Emit) Missing() {
	MissingTotal.With(e.labels()).Inc()
	EventLedger.add(e, func(c *Counts) { c.Missing++ })
}

// Recovered is a delivery that arrived after its event had already been swept
// as missing. It counts as a delivery in every respect — the latency is real
// and belongs in the histogram — and additionally moves the event out of
// Missing, which Delivered has no reason to do.
func (e Emit) Recovered(latency time.Duration) {
	l := e.labels()
	RecoveredTotal.With(l).Inc()
	DeliveryLatency.With(l).Observe(nonNeg(latency))
	DeliveredTotal.With(l).Inc()
	EventLedger.add(e, func(c *Counts) { c.Deliveries++; c.Missing--; c.Completed++ })
}

func (e Emit) Duplicate() {
	DuplicatesTotal.With(e.labels()).Inc()
	EventLedger.add(e, func(c *Counts) { c.Duplicates++ })
}

func (e Emit) Cutoff() {
	CutoffTotal.With(e.labels()).Inc()
	EventLedger.add(e, func(c *Counts) { c.Cutoff++ })
}

func nonNeg(d time.Duration) float64 {
	if d < 0 {
		return 0
	}
	return d.Seconds()
}

func init() {
	Registry.MustRegister(
		PublishLatency, DeliveryLatency, GeneratorLag,
		PublishedTotal, PublishErrorsTotal, DeliveredTotal, EventsCompletedTotal,
		MissingTotal, RecoveredTotal, DuplicatesTotal, CutoffTotal, BytesPublishedTotal,
		InFlight, OfferedRate, RunInfo, RunPhaseStart,
	)
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

package publisher

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"

	"github.com/hookdeck/outpost/loadtest/app/internal/eventlog"
	"github.com/hookdeck/outpost/loadtest/app/internal/group"
	"github.com/hookdeck/outpost/loadtest/app/internal/metrics"
	"github.com/hookdeck/outpost/loadtest/app/internal/outpost"
)

type Publisher struct {
	client  *outpost.Client
	tracker *metrics.InFlightTracker

	runID atomic.Value // string — set by the run controller; "" outside a run

	mu      sync.Mutex
	running map[string]*groupPublisher
}

// SetRunID stamps every subsequent metric with the run it belongs to.
func (p *Publisher) SetRunID(id string) { p.runID.Store(id) }

func (p *Publisher) RunID() string {
	if v, ok := p.runID.Load().(string); ok {
		return v
	}
	return ""
}

// SetPhase attributes subsequently scheduled events to a run phase. Events
// already scheduled keep the phase they were scheduled under, so the steady
// window stays clean at both edges.
func (p *Publisher) SetPhase(groupName, phase string) {
	p.mu.Lock()
	gp, ok := p.running[groupName]
	p.mu.Unlock()
	if ok {
		gp.phase.Store(phase)
	}
}

type groupPublisher struct {
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	limiters []*rate.Limiter
	rate     atomic.Int64
	phase    atomic.Value // string — the run phase events are attributed to
}

// phaseFree is the phase for events published outside a managed run — ad-hoc
// group starts from the dashboard. They are measured, never reported.
const phaseFree = "free"

// publishTimeout bounds a single publish request so a hung call cannot hold a
// group's Stop open indefinitely.
const publishTimeout = 30 * time.Second

func (gp *groupPublisher) currentPhase() string {
	if v, ok := gp.phase.Load().(string); ok {
		return v
	}
	return ""
}

type publishJob struct {
	eventID     string
	tenantID    string
	topic       string
	payload     []byte
	scheduledAt time.Time // t0 — when the token bucket says this event was due
	phase       string
}

func New(client *outpost.Client, tracker *metrics.InFlightTracker) *Publisher {
	return &Publisher{
		client:  client,
		tracker: tracker,
		running: make(map[string]*groupPublisher),
	}
}

func (p *Publisher) Start(g *group.Group) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.running[g.Config.Name]; ok {
		return fmt.Errorf("publisher for group %q already running", g.Config.Name)
	}

	perTenantRate := g.Config.Publish.RatePerTenant
	totalRate := perTenantRate * g.Config.TenantCount

	ctx, cancel := context.WithCancel(context.Background())
	gp := &groupPublisher{
		cancel:   cancel,
		limiters: make([]*rate.Limiter, g.Config.TenantCount),
	}
	gp.rate.Store(int64(totalRate))
	gp.phase.Store(phaseFree)
	metrics.OfferedRate.With(prometheus.Labels{
		"run_id": p.RunID(), "profile": g.Config.Name,
	}).Set(float64(totalRate))

	for i := 0; i < g.Config.TenantCount; i++ {
		limiter := rate.NewLimiter(rate.Limit(perTenantRate), max(perTenantRate, 1))
		gp.limiters[i] = limiter

		// Workers per tenant: match rate 1:1 to handle high-latency targets (cloud APIs)
		workerCount := max(perTenantRate, 2)

		jobs := make(chan publishJob, workerCount*2)

		// Spawn publish workers
		for w := 0; w < workerCount; w++ {
			gp.wg.Add(1)
			go p.publishWorker(ctx, g, &gp.wg, jobs)
		}

		// Spawn tenant manager (rate limiter → job feeder)
		gp.wg.Add(1)
		go p.tenantManager(ctx, g, gp, i, limiter, jobs)
	}

	// Pattern controller
	pattern := newPattern(g.Config.Publish.Pattern, g.Config.Publish.PatternParams)
	go pattern.Start(ctx, &gp.rate, int64(totalRate))

	// Rate syncer
	go p.rateSyncer(ctx, gp, g.Config.TenantCount)

	p.running[g.Config.Name] = gp

	slog.Info("publisher started",
		"group", g.Config.Name,
		"rate_per_tenant", perTenantRate,
		"tenants", g.Config.TenantCount,
		"workers_per_tenant", max(perTenantRate, 2),
		"total_rate", totalRate,
		"pattern", g.Config.Publish.Pattern,
	)
	return nil
}

func (p *Publisher) Stop(g *group.Group) {
	p.mu.Lock()
	gp, ok := p.running[g.Config.Name]
	if !ok {
		p.mu.Unlock()
		return
	}
	delete(p.running, g.Config.Name)
	p.mu.Unlock()

	gp.cancel()
	gp.wg.Wait()
	slog.Info("publisher stopped", "group", g.Config.Name)
}

func (p *Publisher) UpdateRate(groupName string, newRate int) {
	p.mu.Lock()
	gp, ok := p.running[groupName]
	p.mu.Unlock()
	if !ok {
		return
	}
	gp.rate.Store(int64(newRate))
}

func (p *Publisher) rateSyncer(ctx context.Context, gp *groupPublisher, tenantCount int) {
	var lastRate int64
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentRate := gp.rate.Load()
			if currentRate != lastRate {
				perTenant := rate.Limit(currentRate / int64(tenantCount))
				if perTenant < 1 {
					perTenant = 1
				}
				for _, l := range gp.limiters {
					l.SetLimit(perTenant)
					l.SetBurst(max(int(perTenant), 1))
				}
				lastRate = currentRate
			}
		}
	}
}

// tenantManager ticks at the rate limit and feeds jobs to workers.
func (p *Publisher) tenantManager(ctx context.Context, g *group.Group, gp *groupPublisher, tenantIndex int, limiter *rate.Limiter, jobs chan<- publishJob) {
	defer gp.wg.Done()
	defer close(jobs)

	tenantID := g.TenantID(tenantIndex)
	var counter int64

	for {
		// Reserve, rather than Wait, so we learn *when this event was due* —
		// t0 — independently of when we get around to sending it. Latency
		// measured from t0 keeps the generator's own delay in the number
		// instead of hiding it, which is the whole point of running open-loop.
		res := limiter.Reserve()
		if !res.OK() {
			return
		}
		delay := res.Delay()
		scheduledAt := time.Now().Add(delay)
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				res.Cancel()
				return
			}
		}

		counter++

		topic := g.Config.Topics[0]
		if len(g.Config.Topics) > 1 {
			topic = g.Config.Topics[int(counter)%len(g.Config.Topics)]
		}

		eventID := fmt.Sprintf("%s-t%d-%d-%d", g.Config.Name, tenantIndex, counter, time.Now().UnixNano())
		payload := generatePayload(eventID, tenantID, g.Config.Publish.PayloadBytes)

		select {
		case jobs <- publishJob{
			eventID:     eventID,
			tenantID:    tenantID,
			topic:       topic,
			payload:     payload,
			scheduledAt: scheduledAt,
			phase:       gp.currentPhase(),
		}:
		case <-ctx.Done():
			return
		}
	}
}

// publishWorker reads jobs and publishes to Outpost.
func (p *Publisher) publishWorker(ctx context.Context, g *group.Group, wg *sync.WaitGroup, jobs <-chan publishJob) {
	defer wg.Done()

	for {
		select {
		case job, ok := <-jobs:
			if !ok {
				return
			}
			meta := metrics.EventMeta{
				GroupName:   g.Config.Name,
				Phase:       job.phase,
				ScheduledAt: job.scheduledAt,
			}
			emit := metrics.Emit{RunID: p.RunID(), Profile: g.Config.Name, Phase: job.phase}

			// Pre-record so a fast delivery callback can find this event even if
			// publish HTTP latency exceeds outpost queue→deliver latency.
			p.tracker.RecordPublish(job.eventID, meta, g.Config.DestinationsPerTenant)
			g.EventLog.Add(eventlog.Record{
				EventID:     job.eventID,
				GroupName:   g.Config.Name,
				TenantID:    job.tenantID,
				Topic:       job.topic,
				Status:      eventlog.StatusPublished,
				PublishedAt: job.scheduledAt,
			})

			// t1: the request is about to go on the wire. The gap from t0 is
			// generator lag — our delay, not the system's, and reported as such.
			sentAt := time.Now()
			lag := sentAt.Sub(job.scheduledAt)
			g.Metrics.RecordGeneratorLag(lag)
			emit.GeneratorLagged(lag)

			// Detached from the group context on purpose. Stopping a group must
			// not abort a request that is already on the wire: Outpost may have
			// accepted it and gone on to deliver it, and an aborted publish is
			// counted as neither published nor errored — the event then arrives
			// as a delivery with no publish behind it and the ledger cannot
			// balance. Every job now leaves this loop counted exactly once.
			pubCtx, cancelPub := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
			result, err := p.client.Publish(pubCtx, job.tenantID, job.eventID, job.topic, job.payload)
			cancelPub()
			if err != nil {
				// Roll back the pre-record: drop from tracker (no late "missing" sweep),
				// flip eventlog to error so users see it.
				p.tracker.RemoveInFlight(job.eventID)
				g.Metrics.RecordPublishError()
				emit.PublishError()
				g.EventLog.Update(job.eventID, func(r *eventlog.Record) {
					r.Status = eventlog.StatusError
					r.Error = err.Error()
					r.StatusCode = result.StatusCode
				})
				slog.Debug("publish error", "group", g.Config.Name, "tenant", job.tenantID, "error", err)
				continue
			}

			// Publish latency runs from t0, not from t1, so a generator that
			// falls behind cannot flatter the number it reports.
			publishLatency := time.Since(job.scheduledAt)
			g.Metrics.RecordPublish(publishLatency)
			emit.Published(publishLatency, len(job.payload))
			g.EventLog.Update(job.eventID, func(r *eventlog.Record) {
				r.PublishLatency = publishLatency.Milliseconds()
				r.WireLatency = result.Latency.Milliseconds()
			})
		case <-ctx.Done():
			return
		}
	}
}

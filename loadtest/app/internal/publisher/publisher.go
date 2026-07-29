package publisher

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/hookdeck/outpost/loadtest/app/internal/eventlog"
	"github.com/hookdeck/outpost/loadtest/app/internal/group"
	"github.com/hookdeck/outpost/loadtest/app/internal/metrics"
	"github.com/hookdeck/outpost/loadtest/app/internal/outpost"
)

type Publisher struct {
	client  *outpost.Client
	tracker *metrics.InFlightTracker

	mu      sync.Mutex
	running map[string]*groupPublisher
}

type groupPublisher struct {
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	limiters []*rate.Limiter
	rate     atomic.Int64
}

type publishJob struct {
	eventID  string
	tenantID string
	topic    string
	payload  []byte
	sentAt   time.Time
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
		if err := limiter.Wait(ctx); err != nil {
			return
		}

		counter++

		topic := g.Config.Topics[0]
		if len(g.Config.Topics) > 1 {
			topic = g.Config.Topics[int(counter)%len(g.Config.Topics)]
		}

		eventID := fmt.Sprintf("%s-t%d-%d-%d", g.Config.Name, tenantIndex, counter, time.Now().UnixNano())
		payload := generatePayload(eventID, tenantID, g.Config.Publish.PayloadBytes)
		sentAt := time.Now()

		select {
		case jobs <- publishJob{eventID: eventID, tenantID: tenantID, topic: topic, payload: payload, sentAt: sentAt}:
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
			// Pre-record so a fast delivery callback can find this event even if
			// publish HTTP latency exceeds outpost queue→deliver latency.
			p.tracker.RecordPublish(job.eventID, g.Config.Name, g.Config.DestinationsPerTenant, job.sentAt)
			g.EventLog.Add(eventlog.Record{
				EventID:     job.eventID,
				GroupName:   g.Config.Name,
				TenantID:    job.tenantID,
				Topic:       job.topic,
				Status:      eventlog.StatusPublished,
				PublishedAt: job.sentAt,
			})

			result, err := p.client.Publish(ctx, job.tenantID, job.eventID, job.topic, job.payload)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				// Roll back the pre-record: drop from tracker (no late "missing" sweep),
				// flip eventlog to error so users see it.
				p.tracker.RemoveInFlight(job.eventID)
				g.Metrics.RecordPublishError()
				g.EventLog.Update(job.eventID, func(r *eventlog.Record) {
					r.Status = eventlog.StatusError
					r.Error = err.Error()
					r.StatusCode = result.StatusCode
				})
				slog.Debug("publish error", "group", g.Config.Name, "tenant", job.tenantID, "error", err)
				continue
			}
			g.Metrics.RecordPublish(result.Latency)
			g.EventLog.Update(job.eventID, func(r *eventlog.Record) {
				r.PublishLatency = result.Latency.Milliseconds()
			})
		case <-ctx.Done():
			return
		}
	}
}

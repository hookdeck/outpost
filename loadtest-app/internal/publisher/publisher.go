package publisher

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hookdeck/outpost/loadtest-app/internal/group"
	"github.com/hookdeck/outpost/loadtest-app/internal/metrics"
	"github.com/hookdeck/outpost/loadtest-app/internal/outpost"
)

type Publisher struct {
	client  *outpost.Client
	tracker *metrics.InFlightTracker

	mu        sync.Mutex
	running   map[string]*groupPublisher
}

type groupPublisher struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
	rate   atomic.Int64 // events per second for the whole group
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

	totalRate := g.Config.Publish.RatePerTenant * g.Config.TenantCount
	workerCount := totalRate / 500
	if workerCount < 1 {
		workerCount = 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	gp := &groupPublisher{cancel: cancel}
	gp.rate.Store(int64(totalRate))
	p.running[g.Config.Name] = gp

	// Start pattern controller
	pattern := newPattern(g.Config.Publish.Pattern, g.Config.Publish.PatternParams)
	go pattern.Start(ctx, &gp.rate, int64(totalRate))

	for w := 0; w < workerCount; w++ {
		gp.wg.Add(1)
		go p.publishLoop(ctx, g, gp, w)
	}

	slog.Info("publisher started", "group", g.Config.Name, "rate", totalRate, "workers", workerCount, "pattern", g.Config.Publish.Pattern)
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
	if ok {
		gp.rate.Store(int64(newRate))
	}
}

func (p *Publisher) publishLoop(ctx context.Context, g *group.Group, gp *groupPublisher, workerID int) {
	defer gp.wg.Done()

	tenantCount := g.Config.TenantCount
	var counter atomic.Int64

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Simple rate limiting via sleep
		rate := gp.rate.Load()
		if rate <= 0 {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Sleep for inter-event interval
		interval := time.Second / time.Duration(rate)
		time.Sleep(interval)

		// Round-robin tenant selection
		idx := int(counter.Add(1)-1) % tenantCount
		tenantID := g.TenantID(idx)

		// Pick topic (round-robin if multiple)
		topic := g.Config.Topics[0]
		if len(g.Config.Topics) > 1 {
			topic = g.Config.Topics[int(counter.Load())%len(g.Config.Topics)]
		}

		eventID := fmt.Sprintf("%s-%d-%d-%d", g.Config.Name, workerID, counter.Load(), time.Now().UnixNano())
		payload := generatePayload(eventID, tenantID, g.Config.Publish.PayloadBytes)

		expectedDeliveries := g.Config.DestinationsPerTenant
		sentAt := time.Now()

		p.tracker.RecordPublish(eventID, g.Config.Name, expectedDeliveries, sentAt)

		result, err := p.client.Publish(ctx, tenantID, eventID, topic, payload)
		if err != nil {
			if ctx.Err() != nil {
				return // Context cancelled, shutting down
			}
			g.Metrics.RecordPublishError()
			slog.Debug("publish error", "group", g.Config.Name, "error", err)
			continue
		}

		g.Metrics.RecordPublish(result.Latency)
	}
}

package logmq

import (
	"context"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/opevents"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// emitTimeout caps a single sink send. It is the system's definition of the
// worst ACCEPTABLE send latency: the delivery pool is sized to sustain full
// line rate at exactly this latency (see deriveDeliveryConcurrency), so a
// sink can run this slow indefinitely without falling behind. Anything slower
// fails into the nack/redelivery path — no worker count makes a
// beyond-timeout sink work, so sizing stops here.
const emitTimeout = 5 * time.Second

// delivery is one attempt's owed operator events plus the message whose
// terminal state (ack/nack) they decide. Events are fully built before enqueue
// — post-disable, so the payload carries the destination's latest state — and
// one worker owns the whole delivery: its child send goroutines each read one
// event, and the entry is read-only from here on.
type delivery struct {
	// events: disabled, consecutive_failure, exhausted_retries. Sent
	// concurrently — no arrival-order guarantee within the attempt.
	events []deliveryEvent
	// entry supplies the audit-log fields.
	entry *models.LogEntry
	// msg is acked (all events delivered) or nacked (any failure) by the worker.
	msg *mqs.Message
	// processedKey is the per-attempt replay gate key, marked before ack.
	processedKey string
}

type deliveryEvent struct {
	event opevents.Event
	// suppressKey is the exhausted-retries suppression window key; "" = no
	// window (emit unconditionally).
	suppressKey string
}

// deliveryPool delivers operator events on a fixed set of workers, unordered
// across attempts. It exists so the slow part of the alert pipeline (the sink
// call) never blocks the batch loop: eval hands a delivery off and moves on;
// the worker finishes the attempt's gate mark and terminal state.
type deliveryPool struct {
	ctx            context.Context
	logger         *logging.Logger
	emitter        opevents.Emitter
	processedIdemp idempotence.Idempotence
	exhaustedIdemp idempotence.Idempotence

	queue chan delivery
	wg    sync.WaitGroup
}

func newDeliveryPool(ctx context.Context, logger *logging.Logger, alerts AlertPipeline, concurrency, queueDepth int) *deliveryPool {
	p := &deliveryPool{
		ctx:            ctx,
		logger:         logger,
		emitter:        alerts.Emitter,
		processedIdemp: alerts.ProcessedIdemp,
		exhaustedIdemp: alerts.ExhaustedIdemp,
		queue:          make(chan delivery, queueDepth),
	}
	p.wg.Add(concurrency)
	for range concurrency {
		go p.worker()
	}
	return p
}

// enqueue hands a delivery to the pool. It blocks while the queue is full —
// that block is the backpressure path: it stalls the batch loop, which stalls
// the batcher, which stops draining the broker, so excess backlog stays in the
// broker instead of in memory.
func (p *deliveryPool) enqueue(d delivery) {
	p.queue <- d
}

// shutdown stops intake and drains: every enqueued delivery reaches a terminal
// state before shutdown returns. The caller must guarantee no concurrent
// enqueues (the batcher is shut down first).
func (p *deliveryPool) shutdown() {
	close(p.queue)
	p.wg.Wait()
}

func (p *deliveryPool) worker() {
	defer p.wg.Done()
	for d := range p.queue {
		p.process(d)
	}
}

// process emits the attempt's events concurrently, then marks the replay gate
// and acks. The worker still owns the whole attempt — the fan-out is child
// goroutines inside it, so the shared ack needs no fan-in protocol — but the
// worker is occupied ~one send latency instead of K× it, which is what lets
// the pool's sizing ignore how many events an attempt owes. Arrival order
// within an attempt is not guaranteed. Any failure nacks with nothing marked,
// so redelivery re-runs the attempt in full — events already sent may go out
// again (at-least-once).
func (p *deliveryPool) process(d delivery) {
	ctx := p.ctx
	g, gctx := errgroup.WithContext(ctx)
	for _, de := range d.events {
		g.Go(func() error {
			sendCtx, cancel := context.WithTimeout(gctx, emitTimeout)
			defer cancel()
			if err := p.send(sendCtx, de, d.entry); err != nil {
				p.logger.Ctx(ctx).Error("opevent delivery failed",
					zap.Error(err),
					zap.String("topic", de.event.Topic),
					zap.String("attempt_id", d.entry.Attempt.ID),
					zap.String("event_id", d.entry.Event.ID),
					zap.String("destination_id", d.entry.Destination.ID))
				return err
			}
			return nil
		})
	}
	if g.Wait() != nil {
		d.msg.Nack()
		return
	}

	if err := p.processedIdemp.MarkProcessed(ctx, d.processedKey); err != nil {
		p.logger.Ctx(ctx).Error("failed to mark attempt processed",
			zap.Error(err),
			zap.String("attempt_id", d.entry.Attempt.ID),
			zap.String("destination_id", d.entry.Destination.ID))
		d.msg.Nack()
		return
	}
	d.msg.Ack()
}

// send emits one event and audits the send, inside the event's suppression
// window when it has one. A suppressed duplicate (Exec skips the emit) counts
// as delivered and is not audited.
func (p *deliveryPool) send(ctx context.Context, de deliveryEvent, entry *models.LogEntry) error {
	emit := func(ctx context.Context) error {
		if err := p.emitter.Emit(ctx, de.event); err != nil {
			return err
		}
		p.logger.Ctx(ctx).Audit("opevent delivered",
			zap.String("topic", de.event.Topic),
			zap.String("attempt_id", entry.Attempt.ID),
			zap.String("event_id", entry.Event.ID),
			zap.String("tenant_id", de.event.TenantID),
			zap.String("destination_id", entry.Destination.ID),
			zap.String("destination_type", entry.Destination.Type))
		return nil
	}
	if de.suppressKey == "" {
		return emit(ctx)
	}
	return p.exhaustedIdemp.Exec(ctx, de.suppressKey, emit)
}

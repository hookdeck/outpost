package logmq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/mikestefanello/batcher"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// ErrInvalidLogEntry is returned when a LogEntry is missing required fields.
var ErrInvalidLogEntry = errors.New("invalid log entry: both event and attempt are required")

// emitTimeout caps a single sink send. It is the system's definition of the
// worst acceptable send latency, and it bounds each entry goroutine's
// lifetime: in-flight work steady-states at arrival rate × this timeout even
// when every send is stuck, and the shutdown drain is bounded by it. Anything
// slower fails into the nack/redelivery path.
const emitTimeout = 5 * time.Second

// LogStore defines the interface for persisting log entries.
// This is a consumer-defined interface containing only what logmq needs.
type LogStore interface {
	InsertMany(ctx context.Context, entries []*models.LogEntry) error
}

// AlertEvaluator evaluates one delivery attempt against the destination's
// failure history and returns the tracker's verdict as data. Acting on the
// verdict (opevents, auto-disable, replay dedup) is owned by the batch
// processor.
type AlertEvaluator interface {
	Evaluate(ctx context.Context, attempt alert.Attempt) (alert.Evaluation, error)
}

// DestinationDisabler disables destinations that hit the auto-disable
// threshold.
type DestinationDisabler interface {
	DisableDestination(ctx context.Context, tenantID, destinationID string) error
}

// ReplayGate is the split-phase idempotence pair the pipeline uses as the
// per-attempt replay gate: Processed is checked before eval, MarkProcessed
// lands after delivery. Split-phase means no in-flight conflict detection —
// concurrent duplicates both run and may both emit (tolerated: opevents are
// at-least-once). Satisfied by idempotence.Idempotence.
type ReplayGate interface {
	Processed(ctx context.Context, key string) (bool, error)
	MarkProcessed(ctx context.Context, key string) error
}

// SuppressionWindow wraps one send in a keyed dedup window: within the window
// the send is skipped and counts as delivered. Satisfied by
// idempotence.Idempotence.
type SuppressionWindow interface {
	Exec(ctx context.Context, key string, exec func(context.Context) error) error
}

// AlertPipeline groups the post-persist alert pipeline: evaluate the attempt,
// act on the verdict (disable, opevents), and dedup replays.
type AlertPipeline struct {
	// Evaluator is the alert tracker. Nil disables the pipeline entirely.
	Evaluator AlertEvaluator
	// Emitter delivers the operator events. Required when Evaluator is set.
	Emitter opevents.Emitter
	// Disabler auto-disables a destination when the 100% threshold is crossed.
	// Nil disables auto-disable.
	Disabler DestinationDisabler
	// ProcessedIdemp is the per-attempt replay gate: a replay of a fully
	// processed failed attempt is skipped instead of re-counting/re-alerting.
	// Required when Evaluator is set.
	ProcessedIdemp ReplayGate
	// ExhaustedIdemp is the per-(event,destination) suppression window for
	// exhausted-retries alerts. Nil means no suppression (alert on every
	// exhaustion).
	ExhaustedIdemp SuppressionWindow
}

// BatchProcessorConfig configures the batch processor.
type BatchProcessorConfig struct {
	ItemCountThreshold int
	DelayThreshold     time.Duration
	// EmitTimeout is a test-only override for the per-send timeout; zero means
	// the emitTimeout default. Production always runs the default.
	EmitTimeout time.Duration
}

// BatchProcessor batches log entries and writes them to the log store, then
// runs the alert pipeline per entry on its own goroutine — dispatch-and-move-on,
// so a slow eval or sink send never blocks persistence of the next batch.
//
// Entries process in no particular order, including within a destination: the
// consecutive-failure count tolerates approximate order (its store is a set of
// attempt IDs, so counting is idempotent and commutative; only a success/
// failure race around the reset can skew it), and per-process ordering was
// cosmetic anyway with multiple logmq replicas interleaving a destination's
// attempts. See the discussion on the parallelism RFC.
type BatchProcessor struct {
	ctx         context.Context
	logger      *logging.Logger
	logStore    LogStore
	alerts      AlertPipeline
	batcher     *batcher.Batcher[*mqs.Message]
	emitTimeout time.Duration
	// inflight tracks entry goroutines so Shutdown can drain them. Each is
	// bounded by emitTimeout, so the wait is bounded too.
	inflight     sync.WaitGroup
	shutdownOnce sync.Once
}

// NewBatchProcessor creates a new batch processor for log entries.
func NewBatchProcessor(ctx context.Context, logger *logging.Logger, logStore LogStore, alerts AlertPipeline, cfg BatchProcessorConfig) (*BatchProcessor, error) {
	if alerts.Evaluator != nil {
		if alerts.Emitter == nil {
			return nil, errors.New("logmq: AlertPipeline requires an Emitter when Evaluator is set")
		}
		if alerts.ProcessedIdemp == nil {
			return nil, errors.New("logmq: AlertPipeline requires a ProcessedIdemp when Evaluator is set")
		}
	}
	bp := &BatchProcessor{
		ctx:         ctx,
		logger:      logger,
		logStore:    logStore,
		alerts:      alerts,
		emitTimeout: cfg.EmitTimeout,
	}
	if bp.emitTimeout <= 0 {
		bp.emitTimeout = emitTimeout
	}

	b, err := batcher.NewBatcher(batcher.Config[*mqs.Message]{
		GroupCountThreshold: 2,
		ItemCountThreshold:  cfg.ItemCountThreshold,
		DelayThreshold:      cfg.DelayThreshold,
		NumGoroutines:       1,
		Processor:           bp.processBatch,
	})
	if err != nil {
		return nil, err
	}

	bp.batcher = b
	return bp, nil
}

// Add adds a message to the batch.
func (bp *BatchProcessor) Add(ctx context.Context, msg *mqs.Message) error {
	bp.batcher.Add("", msg)
	return nil
}

// Shutdown gracefully shuts down the batch processor: the batcher first
// (flushes pending batches, which may still dispatch entry goroutines), then
// the in-flight entries drain. Every dispatched message reaches a terminal
// state before Shutdown returns, and the drain is bounded by emitTimeout.
// Idempotent.
func (bp *BatchProcessor) Shutdown() {
	bp.shutdownOnce.Do(func() {
		bp.batcher.Shutdown()
		bp.inflight.Wait()
	})
}

// processBatch processes a batch of messages.
func (bp *BatchProcessor) processBatch(_ string, msgs []*mqs.Message) {
	logger := bp.logger.Ctx(bp.ctx)
	logger.Debug("processing batch", zap.Int("message_count", len(msgs)))

	entries := make([]*models.LogEntry, 0, len(msgs))
	validMsgs := make([]*mqs.Message, 0, len(msgs))
	seenAttempts := make(map[string]struct{}, len(msgs))

	for _, msg := range msgs {
		entry := &models.LogEntry{}
		if err := entry.FromMessage(msg); err != nil {
			logger.Error("failed to parse log entry",
				zap.Error(err),
				zap.String("message_id", msg.LoggableID))
			msg.Nack()
			continue
		}

		// Validate that both Event and Attempt are present.
		// The logstore requires both for data consistency.
		if entry.Event == nil || entry.Attempt == nil {
			fields := []zap.Field{
				zap.Bool("has_event", entry.Event != nil),
				zap.Bool("has_attempt", entry.Attempt != nil),
				zap.String("message_id", msg.LoggableID),
			}
			if entry.Event != nil {
				fields = append(fields, zap.String("event_id", entry.Event.ID))
				fields = append(fields, zap.String("tenant_id", entry.Event.TenantID))
			}
			if entry.Attempt != nil {
				fields = append(fields, zap.String("attempt_id", entry.Attempt.ID))
				fields = append(fields, zap.String("tenant_id", entry.Attempt.TenantID))
			}
			logger.Error("invalid log entry: both event and attempt are required", fields...)
			msg.Nack()
			continue
		}

		// Dedup duplicate copies of the same attempt within the batch
		// (at-least-once redelivery, producer re-publish — possibly under
		// different MQ message IDs). Copies are byte-identical, so the
		// duplicate is acked immediately; the at-least-once guarantee rides
		// on the kept copy, which stays un-acked until persisted.
		if _, ok := seenAttempts[entry.Attempt.ID]; ok {
			logger.Debug("duplicate log entry in batch",
				zap.String("message_id", msg.LoggableID),
				zap.String("attempt_id", entry.Attempt.ID),
				zap.String("event_id", entry.Event.ID),
				zap.String("tenant_id", entry.Event.TenantID))
			msg.Ack()
			continue
		}
		seenAttempts[entry.Attempt.ID] = struct{}{}

		logger.Debug("added to batch",
			zap.String("message_id", msg.LoggableID),
			zap.String("event_id", entry.Event.ID),
			zap.String("attempt_id", entry.Attempt.ID),
			zap.String("tenant_id", entry.Event.TenantID))

		entries = append(entries, entry)
		validMsgs = append(validMsgs, msg)
	}

	// Nothing valid to insert
	if len(entries) == 0 {
		return
	}

	insertCtx, cancel := context.WithTimeout(bp.ctx, 30*time.Second)
	defer cancel()

	insertStart := time.Now()
	if err := bp.logStore.InsertMany(insertCtx, entries); err != nil {
		logger.Error("failed to insert log entries",
			zap.Error(err),
			zap.Int("entry_count", len(entries)),
			zap.Int64("insert_duration_ms", time.Since(insertStart).Milliseconds()))
		for _, msg := range validMsgs {
			msg.Nack()
		}
		return
	}

	logger.Info("batch persisted",
		zap.Int("count", len(validMsgs)),
		zap.Int64("insert_duration_ms", time.Since(insertStart).Milliseconds()))

	// Spawn one goroutine per persisted entry and return — the batch loop
	// never waits on eval or delivery. In-flight goroutines are bounded by
	// arrival rate × emitTimeout (each lives at most about one send latency),
	// and every fetched message reaches ack/nack well inside the broker's
	// visibility window.
	for i, entry := range entries {
		if bp.alerts.Evaluator == nil {
			validMsgs[i].Ack()
			continue
		}

		// Graceful nil: skip alert eval if no destination.
		// This only happens during the initial migration when older deliverymq
		// instances haven't been updated to populate LogEntry.Destination yet.
		// Can be removed after v1.0.
		if entry.Destination == nil {
			validMsgs[i].Ack()
			continue
		}

		msg := validMsgs[i]
		bp.inflight.Go(func() {
			bp.processEntry(bp.ctx, entry, msg)
		})
	}
}

// processEntry runs the alert pipeline for one persisted entry and owns the
// message's terminal state: evaluate the attempt, act on the verdict (disable,
// build the operator events), deliver the events, ack/nack.
//
// A failed attempt runs inside the per-attempt processed gate, so a replay
// (MQ redelivery, producer re-publish) of a fully processed attempt is skipped
// instead of re-counting or re-alerting. The check runs BEFORE eval — a stale
// replay arriving after a success reset must not count toward the fresh
// streak. The mark lands only after the attempt's events are delivered — a
// nacked attempt re-runs in full on redelivery (counting stays correct: the
// store is idempotent per attempt ID). A success resets the tracker and emits
// attempt.success — both idempotent-enough to skip the gate (gating would cost
// one Redis key per successful attempt, the dominant traffic, to dedup a rare
// redelivery re-emit; opevents are at-least-once anyway).
func (bp *BatchProcessor) processEntry(ctx context.Context, entry *models.LogEntry, msg *mqs.Message) {
	attempt := alert.Attempt{
		TenantID:         entry.Destination.TenantID,
		DestinationID:    entry.Destination.ID,
		AttemptID:        entry.Attempt.ID,
		Number:           entry.Attempt.AttemptNumber,
		Success:          entry.Attempt.Status == models.AttemptStatusSuccess,
		EligibleForRetry: entry.Event.EligibleForRetry,
	}

	if attempt.Success {
		if _, err := bp.alerts.Evaluator.Evaluate(ctx, attempt); err != nil {
			bp.nackAlertFailure(ctx, err, entry, msg)
			return
		}
		success := deliveryEvent{
			event: opevents.AttemptSuccessEvent(opevents.NewAlertDestination(entry.Destination), entry.Event, entry.Attempt),
		}
		if bp.sendAll(ctx, []deliveryEvent{success}, entry) != nil {
			msg.Nack()
			return
		}
		msg.Ack()
		return
	}

	key := processedKey(attempt.AttemptID)
	processed, err := bp.alerts.ProcessedIdemp.Processed(ctx, key)
	if err != nil {
		bp.nackAlertFailure(ctx, err, entry, msg)
		return
	}
	if processed {
		msg.Ack()
		return
	}

	eval, err := bp.alerts.Evaluator.Evaluate(ctx, attempt)
	if err != nil {
		bp.nackAlertFailure(ctx, err, entry, msg)
		return
	}

	events, err := bp.plan(ctx, eval, entry)
	if err != nil {
		bp.nackAlertFailure(ctx, err, entry, msg)
		return
	}

	// Any failure nacks with nothing marked, so redelivery re-runs the attempt
	// in full — events already sent may go out again (at-least-once).
	if bp.sendAll(ctx, events, entry) != nil {
		msg.Nack()
		return
	}

	if err := bp.alerts.ProcessedIdemp.MarkProcessed(ctx, key); err != nil {
		bp.logger.Ctx(ctx).Error("failed to mark attempt processed",
			zap.Error(err),
			zap.String("attempt_id", entry.Attempt.ID),
			zap.String("destination_id", entry.Destination.ID))
		msg.Nack()
		return
	}
	msg.Ack()
}

// deliveryEvent is one operator event owed by an attempt.
type deliveryEvent struct {
	event opevents.Event
	// suppressKey is the exhausted-retries suppression window key; "" = no
	// window (emit unconditionally).
	suppressKey string
}

// sendAll delivers an attempt's events concurrently, each under the emit
// timeout; arrival order within an attempt is not guaranteed. The first
// failure is returned (the rest still run to completion or cancellation).
func (bp *BatchProcessor) sendAll(ctx context.Context, events []deliveryEvent, entry *models.LogEntry) error {
	g, gctx := errgroup.WithContext(ctx)
	for _, de := range events {
		g.Go(func() error {
			sendCtx, cancel := context.WithTimeout(gctx, bp.emitTimeout)
			defer cancel()
			if err := bp.send(sendCtx, de, entry); err != nil {
				bp.logger.Ctx(ctx).Error("opevent delivery failed",
					zap.Error(err),
					zap.String("topic", de.event.Topic),
					zap.String("attempt_id", entry.Attempt.ID),
					zap.String("event_id", entry.Event.ID),
					zap.String("destination_id", entry.Destination.ID))
				return err
			}
			return nil
		})
	}
	return g.Wait()
}

// plan acts on an evaluation and builds the operator events owed for this
// attempt — attempt.failed always, plus disabled, consecutive_failure, and
// exhausted_retries per the verdict. They are sent concurrently, so slice
// order carries no meaning. The disable (a DB write) happens here: it's an
// action, not a notification, and it must precede event construction so the
// payloads carry the destination's latest state (disabled) — attempt.failed
// included, since they share the projection.
func (bp *BatchProcessor) plan(ctx context.Context, eval alert.Evaluation, entry *models.LogEntry) ([]deliveryEvent, error) {
	dest := opevents.NewAlertDestination(entry.Destination)
	var events []deliveryEvent

	if cf := eval.ConsecutiveFailure; cf != nil {
		if cf.Level == 100 && bp.alerts.Disabler != nil {
			// Disable converges on replay: re-disabling rewrites DisabledAt,
			// but the end state is the same.
			if err := bp.alerts.Disabler.DisableDestination(ctx, dest.TenantID, dest.ID); err != nil {
				return nil, fmt.Errorf("failed to disable destination: %w", err)
			}

			// The payload carries the destination's latest state: disabled.
			now := time.Now()
			dest.DisabledAt = &now

			bp.logger.Ctx(ctx).Audit("destination disabled",
				zap.String("attempt_id", entry.Attempt.ID),
				zap.String("event_id", entry.Event.ID),
				zap.String("tenant_id", dest.TenantID),
				zap.String("destination_id", dest.ID),
				zap.String("destination_type", dest.Type))

			events = append(events, deliveryEvent{
				event: opevents.DestinationDisabledEvent(dest, entry.Event, entry.Attempt, now),
			})
		}

		events = append(events, deliveryEvent{
			event: opevents.ConsecutiveFailureEvent(dest, entry.Event, entry.Attempt,
				cf.Failures, cf.Max, cf.Level),
		})
	}

	if eval.RetriesExhausted {
		de := deliveryEvent{
			event: opevents.ExhaustedRetriesEvent(dest, entry.Event, entry.Attempt),
		}
		if bp.alerts.ExhaustedIdemp != nil {
			de.suppressKey = exhaustedRetriesKey(entry.Event.ID, dest.ID)
		}
		events = append(events, de)
	}

	events = append(events, deliveryEvent{
		event: opevents.AttemptFailedEvent(dest, entry.Event, entry.Attempt),
	})

	return events, nil
}

// send emits one event and audits the send, inside the event's suppression
// window when it has one. A suppressed duplicate (Exec skips the emit) counts
// as delivered and is not audited.
func (bp *BatchProcessor) send(ctx context.Context, de deliveryEvent, entry *models.LogEntry) error {
	emit := func(ctx context.Context) error {
		if err := bp.alerts.Emitter.Emit(ctx, de.event); err != nil {
			return err
		}
		bp.logger.Ctx(ctx).Audit("opevent delivered",
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
	return bp.alerts.ExhaustedIdemp.Exec(ctx, de.suppressKey, emit)
}

// nackAlertFailure logs an alert-pipeline failure and nacks. InsertMany is
// idempotent (upsert by attempt ID) and a failed attempt is never marked
// processed, so redelivery re-evaluates and re-emits — events already sent may
// go out again (at-least-once).
func (bp *BatchProcessor) nackAlertFailure(ctx context.Context, err error, entry *models.LogEntry, msg *mqs.Message) {
	bp.logger.Ctx(ctx).Error("alert processing failed",
		zap.Error(err),
		zap.String("attempt_id", entry.Attempt.ID),
		zap.String("event_id", entry.Event.ID),
		zap.String("destination_id", entry.Destination.ID))
	msg.Nack()
}

// processedKey is the per-attempt replay gate key. Format is stable — changing
// it re-processes in-window replays.
func processedKey(attemptID string) string {
	return "logmq:processed:" + attemptID
}

// exhaustedRetriesKey is the per-(event,destination) suppression key for
// exhausted-retries alerts. Format is stable — changing it resets live windows.
func exhaustedRetriesKey(eventID, destinationID string) string {
	return "opevents:exhausted:" + eventID + ":" + destinationID
}

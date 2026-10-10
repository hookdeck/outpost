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
	// SignalsEnabled reports whether any alert signal can ever fire. When
	// false, Evaluate is a stateless no-op, so the pipeline skips the replay
	// gate (there is no streak or verdict to protect from replays).
	SignalsEnabled() bool
}

// DestinationDisabler disables destinations that hit the auto-disable
// threshold.
type DestinationDisabler interface {
	DisableDestination(ctx context.Context, tenantID, destinationID string) error
}

// ConditionalDestinationDisabler is a DestinationDisabler that disables only
// a live, enabled destination and reports whether its call did. When it
// didn't (already disabled, deleted or gone), the pipeline emits no
// alert.destination.disabled event: the destination was disabled by an
// earlier attempt, or there is nothing left to disable.
type ConditionalDestinationDisabler interface {
	DestinationDisabler
	DisableDestinationIfEnabled(ctx context.Context, tenantID, destinationID string) (changed bool, err error)
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
// redisSuppressionWindow.
type SuppressionWindow interface {
	Exec(ctx context.Context, key string, exec func(context.Context) error) error
}

// AlertPipeline groups the post-persist alert pipeline: evaluate the attempt,
// act on the verdict (disable, opevents), and dedup replays. Evaluator,
// Emitter, and ProcessedIdemp are always required — "alerting off" is
// expressed by config (signals disabled, no topics subscribed), never by
// absent deps.
type AlertPipeline struct {
	// Evaluator is the alert tracker. Required.
	Evaluator AlertEvaluator
	// Emitter delivers the operator events. Required.
	Emitter opevents.Emitter
	// Disabler auto-disables a destination when the 100% threshold is crossed.
	// Nil disables auto-disable.
	Disabler DestinationDisabler
	// ProcessedIdemp is the per-attempt replay gate: a replay of a fully
	// processed failed attempt is skipped instead of re-counting/re-alerting.
	// Required.
	ProcessedIdemp ReplayGate
	// ExhaustedIdemp is the per-(tenant,destination) suppression window for
	// exhausted-retries alerts: at most one alert per destination within the
	// window, regardless of which events exhaust. Nil means no suppression
	// (alert on every exhaustion).
	ExhaustedIdemp SuppressionWindow
}

// AttemptStatusRecorder keeps a per-destination record of the latest delivery
// attempts, fed with each persisted batch's entries for the configured
// destination types. Best effort: errors are logged, never nacked.
type AttemptStatusRecorder interface {
	RecordAttempts(ctx context.Context, entries []*models.LogEntry) error
}

// statusRecordTimeout bounds one RecordAttempts call.
const statusRecordTimeout = 5 * time.Second

// SEP-3415: a webhook receiver answers 410 Gone to reject one event (stale,
// duplicate) "without affecting the subscription itself", so an MCP 410 never
// touches the destination's consecutive-failure streak.
const (
	mcpDestinationType = "mcp"
	mcpRejectedCode    = "410"
)

// mcpOutpostRefusals are the codes of MCP attempts Outpost failed on its own
// side: the callback host was at Outpost's in-flight limit (throttled), or
// the event can't be sent as an envelope. They say nothing about the
// receiver, so they never touch the consecutive-failure streak and never
// reach the status record behind deliveryStatus.lastError.
var mcpOutpostRefusals = map[string]bool{
	"throttled":         true,
	"payload_too_large": true,
	"invalid_event_id":  true,
}

// isMCPOutpostRefusal reports whether entry is a failed MCP attempt that
// Outpost refused on its own side.
func isMCPOutpostRefusal(entry *models.LogEntry) bool {
	return entry.Attempt.Status != models.AttemptStatusSuccess &&
		entryDestinationType(entry) == mcpDestinationType &&
		mcpOutpostRefusals[entry.Attempt.Code]
}

// entryDestinationType is the destination type of entry's attempt.
func entryDestinationType(entry *models.LogEntry) string {
	if entry.Attempt.DestinationType != "" || entry.Destination == nil {
		return entry.Attempt.DestinationType
	}
	return entry.Destination.Type
}

// BatchProcessorOption configures optional batch processor behaviour.
type BatchProcessorOption func(*BatchProcessor)

// WithTypeMaxRetries sets per-destination-type retry limits, carried to the
// alert evaluator in alert.Attempt.MaxRetries (types not listed use the
// evaluator's default). Pair it with alert.WithTypeMaxRetries so the
// evaluator's SignalsEnabled accounts for them.
func WithTypeMaxRetries(limits map[string]int) BatchProcessorOption {
	return func(bp *BatchProcessor) {
		bp.typeMaxRetries = make(map[string]int, len(limits))
		for t, limit := range limits {
			bp.typeMaxRetries[t] = limit
		}
	}
}

// WithAttemptStatusRecorder records the persisted attempts of the given
// destination types with recorder.
func WithAttemptStatusRecorder(recorder AttemptStatusRecorder, destinationTypes ...string) BatchProcessorOption {
	return func(bp *BatchProcessor) {
		bp.statusRecorder = recorder
		bp.statusTypes = make(map[string]bool, len(destinationTypes))
		for _, t := range destinationTypes {
			bp.statusTypes[t] = true
		}
	}
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
	// alertsEnabled and emitsAttemptEvents are derived once from the pipeline's
	// static config. alertsEnabled=false skips the replay gate on the failed
	// path (nothing to protect); both false skips per-entry work entirely.
	alertsEnabled      bool
	emitsAttemptEvents bool
	// inflight tracks entry goroutines so Shutdown can drain them. Each is
	// bounded by emitTimeout, so the wait is bounded too.
	inflight     sync.WaitGroup
	shutdownOnce sync.Once

	typeMaxRetries map[string]int
	statusRecorder AttemptStatusRecorder
	statusTypes    map[string]bool
}

// NewBatchProcessor creates a new batch processor for log entries.
func NewBatchProcessor(ctx context.Context, logger *logging.Logger, logStore LogStore, alerts AlertPipeline, cfg BatchProcessorConfig, opts ...BatchProcessorOption) (*BatchProcessor, error) {
	if alerts.Evaluator == nil {
		return nil, errors.New("logmq: AlertPipeline requires an Evaluator")
	}
	if alerts.Emitter == nil {
		return nil, errors.New("logmq: AlertPipeline requires an Emitter")
	}
	if alerts.ProcessedIdemp == nil {
		return nil, errors.New("logmq: AlertPipeline requires a ProcessedIdemp")
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
	for _, opt := range opts {
		opt(bp)
	}
	bp.alertsEnabled = alerts.Evaluator.SignalsEnabled()
	bp.emitsAttemptEvents = alerts.Emitter.Enabled(opevents.TopicAttemptSuccess) ||
		alerts.Emitter.Enabled(opevents.TopicAttemptFailed)

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

	bp.recordStatus(entries)

	// Spawn one goroutine per persisted entry and return — the batch loop
	// never waits on eval or delivery. In-flight goroutines are bounded by
	// arrival rate × emitTimeout (each lives at most about one send latency),
	// and every fetched message reaches ack/nack well inside the broker's
	// visibility window.
	for i, entry := range entries {
		// A pipeline that can't produce anything (every alert signal off and
		// no attempt topic subscribed): persisted is terminal.
		if !bp.alertsEnabled && !bp.emitsAttemptEvents {
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
// redelivery re-emit; opevents are at-least-once anyway). The gate exists for
// alert state and alert-event dedup only, so when every signal is disabled the
// failed path skips it too and just emits attempt.failed.
func (bp *BatchProcessor) processEntry(ctx context.Context, entry *models.LogEntry, msg *mqs.Message) {
	attempt := alert.Attempt{
		TenantID:         entry.Destination.TenantID,
		DestinationID:    entry.Destination.ID,
		AttemptID:        entry.Attempt.ID,
		Number:           entry.Attempt.AttemptNumber,
		Success:          entry.Attempt.Status == models.AttemptStatusSuccess,
		EligibleForRetry: entry.Event.EligibleForRetry,
		MaxRetries:       bp.maxRetriesFor(entry.Destination.Type),
	}
	attempt.SkipConsecutiveFailure = !attempt.Success &&
		entry.Destination.Type == mcpDestinationType &&
		(entry.Attempt.Code == mcpRejectedCode || mcpOutpostRefusals[entry.Attempt.Code])

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

	// With every alert signal off there is no streak to protect and no verdict
	// to compute, so the replay gate buys nothing — skip its two Redis round
	// trips and emit attempt.failed ungated, same at-least-once treatment as
	// attempt.success (a redelivery may re-emit; tolerated).
	if !bp.alertsEnabled {
		failed := deliveryEvent{
			event: opevents.AttemptFailedEvent(opevents.NewAlertDestination(entry.Destination), entry.Event, entry.Attempt),
		}
		if bp.sendAll(ctx, []deliveryEvent{failed}, entry) != nil {
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
			if err := bp.send(sendCtx, de); err != nil {
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

// disable auto-disables a destination and reports whether this call changed
// it. A plain DestinationDisabler converges on replay (re-disabling rewrites
// DisabledAt, but the end state is the same) and always reports a change; a
// ConditionalDestinationDisabler reports none when the destination was
// already disabled, deleted or gone.
func (bp *BatchProcessor) disable(ctx context.Context, tenantID, destinationID string) (bool, error) {
	if cd, ok := bp.alerts.Disabler.(ConditionalDestinationDisabler); ok {
		return cd.DisableDestinationIfEnabled(ctx, tenantID, destinationID)
	}
	return true, bp.alerts.Disabler.DisableDestination(ctx, tenantID, destinationID)
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
			changed, err := bp.disable(ctx, dest.TenantID, dest.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to disable destination: %w", err)
			}

			// The payload carries the destination's latest state: disabled.
			now := time.Now()
			dest.DisabledAt = &now

			if changed {
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
			de.suppressKey = exhaustedRetriesKey(dest.TenantID, dest.ID)
		}
		events = append(events, de)
	}

	events = append(events, deliveryEvent{
		event: opevents.AttemptFailedEvent(dest, entry.Event, entry.Attempt),
	})

	return events, nil
}

// send emits one event, inside the event's suppression window when it has
// one. A suppressed duplicate (Exec skips the emit) counts as delivered. The
// emitter owns the delivery audit log — it fires iff an event actually went
// out, so filtered topics and suppressed duplicates leave no line.
func (bp *BatchProcessor) send(ctx context.Context, de deliveryEvent) error {
	// Filter before entering a suppression window. Emit also filters as a
	// boundary safeguard, but an unsubscribed event must not touch Redis or
	// turn a suppression failure into a delivery failure for an event that
	// would never be sent.
	if !bp.alerts.Emitter.Enabled(de.event.Topic) {
		return nil
	}

	emit := func(ctx context.Context) error {
		return bp.alerts.Emitter.Emit(ctx, de.event)
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

// maxRetriesFor maps the destination type's retry limit to
// alert.Attempt.MaxRetries: 0 (evaluator default) for types without their
// own, negative when the type's limit is 0 (no retries).
func (bp *BatchProcessor) maxRetriesFor(destinationType string) int {
	limit, ok := bp.typeMaxRetries[destinationType]
	if !ok {
		return 0
	}
	if limit <= 0 {
		return -1
	}
	return limit
}

// recordStatus hands the batch's persisted entries of the recorded
// destination types to the status recorder on a tracked goroutine, so the
// batch loop never waits on it, leaving out MCP attempts Outpost refused on
// its own side. Best effort: a failure is logged and the messages' fate is
// unaffected.
func (bp *BatchProcessor) recordStatus(entries []*models.LogEntry) {
	if bp.statusRecorder == nil {
		return
	}
	var recorded []*models.LogEntry
	for _, entry := range entries {
		if bp.statusTypes[entryDestinationType(entry)] && !isMCPOutpostRefusal(entry) {
			recorded = append(recorded, entry)
		}
	}
	if len(recorded) == 0 {
		return
	}
	bp.inflight.Go(func() {
		ctx, cancel := context.WithTimeout(bp.ctx, statusRecordTimeout)
		defer cancel()
		if err := bp.statusRecorder.RecordAttempts(ctx, recorded); err != nil {
			bp.logger.Ctx(ctx).Warn("failed to record attempt status",
				zap.Error(err),
				zap.Int("entry_count", len(recorded)))
		}
	})
}

// processedKey is the per-attempt replay gate key. Format is stable — changing
// it re-processes in-window replays.
func processedKey(attemptID string) string {
	return "logmq:processed:" + attemptID
}

// exhaustedRetriesKey is the per-(tenant,destination) suppression key for
// exhausted-retries alerts. Tenant scoping prevents cross-tenant collisions on
// user-supplied destination IDs. Format is stable — changing it resets live
// windows.
func exhaustedRetriesKey(tenantID, destinationID string) string {
	return "opevents:exhausted:" + tenantID + ":" + destinationID
}

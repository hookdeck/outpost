package logmq

import (
	"context"
	"errors"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/mikestefanello/batcher"
	"go.uber.org/zap"
)

// ErrInvalidLogEntry is returned when a LogEntry is missing required fields.
var ErrInvalidLogEntry = errors.New("invalid log entry: both event and attempt are required")

// LogStore defines the interface for persisting log entries.
// This is a consumer-defined interface containing only what logmq needs.
type LogStore interface {
	InsertMany(ctx context.Context, entries []*models.LogEntry) error
}

// AlertMonitor evaluates delivery attempts and returns the alerts to deliver as
// data. Delivery (emit + suppression) is owned by the batch processor.
type AlertMonitor interface {
	Evaluate(ctx context.Context, attempt alert.DeliveryAttempt) (alert.Evaluation, error)
}

// BatchProcessorConfig configures the batch processor.
type BatchProcessorConfig struct {
	ItemCountThreshold int
	DelayThreshold     time.Duration
}

// BatchProcessor batches log entries and writes them to the log store.
type BatchProcessor struct {
	ctx          context.Context
	logger       *logging.Logger
	logStore     LogStore
	alertMonitor AlertMonitor
	emitter      opevents.Emitter
	idemp        idempotence.Idempotence
	batcher      *batcher.Batcher[*mqs.Message]
}

// NewBatchProcessor creates a new batch processor for log entries. When
// alertMonitor is non-nil, emitter delivers the evaluated alert events; idemp
// (may be nil) enforces the exhausted-retries suppression window.
func NewBatchProcessor(ctx context.Context, logger *logging.Logger, logStore LogStore, alertMonitor AlertMonitor, emitter opevents.Emitter, idemp idempotence.Idempotence, cfg BatchProcessorConfig) (*BatchProcessor, error) {
	bp := &BatchProcessor{
		ctx:          ctx,
		logger:       logger,
		logStore:     logStore,
		alertMonitor: alertMonitor,
		emitter:      emitter,
		idemp:        idemp,
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

// Shutdown gracefully shuts down the batch processor.
func (bp *BatchProcessor) Shutdown() {
	bp.batcher.Shutdown()
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

	// Per-entry alert evaluation + delivery after successful persistence.
	for i, entry := range entries {
		if bp.alertMonitor == nil {
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

		da := alert.DeliveryAttempt{
			Event:       entry.Event,
			Destination: alert.AlertDestinationFromDestination(entry.Destination),
			Attempt:     entry.Attempt,
		}
		eval, err := bp.alertMonitor.Evaluate(bp.ctx, da)
		if err != nil {
			logger.Error("alert evaluation failed",
				zap.Error(err),
				zap.String("attempt_id", entry.Attempt.ID),
				zap.String("event_id", entry.Event.ID),
				zap.String("destination_id", entry.Destination.ID))
			// Nack so the message is redelivered. InsertMany is idempotent
			// (upsert by attempt ID), so redelivery won't produce duplicate log entries.
			validMsgs[i].Nack()
			continue
		}

		if err := bp.deliver(bp.ctx, eval, entry); err != nil {
			logger.Error("opevent delivery failed",
				zap.Error(err),
				zap.String("attempt_id", entry.Attempt.ID),
				zap.String("event_id", entry.Event.ID),
				zap.String("destination_id", entry.Destination.ID))
			// Nack so the message is redelivered. The commit (mark-evaluated)
			// runs only after all events deliver, so redelivery re-evaluates and
			// re-emits any events not yet sent.
			validMsgs[i].Nack()
			continue
		}

		validMsgs[i].Ack()
	}
}

// deliver emits an evaluation's events and, on success, runs its commit.
// Exhausted-retries alerts are suppressed per (event, destination) within the
// configured window — recognizing them and owning the suppression key/window is
// a delivery concern, so it lives here rather than on the event. A suppressed
// duplicate is treated as delivered. The commit (mark-evaluated) runs strictly
// AFTER all events deliver.
func (bp *BatchProcessor) deliver(ctx context.Context, eval alert.Evaluation, entry *models.LogEntry) error {
	for _, ev := range eval.Events {
		if ev.Topic == opevents.TopicAlertExhaustedRetries && bp.idemp != nil {
			key := exhaustedRetriesKey(entry.Event.ID, entry.Destination.ID)
			if err := bp.idemp.Exec(ctx, key, func(ctx context.Context) error {
				return bp.emit(ctx, ev, entry)
			}); err != nil {
				return err
			}
			continue
		}
		if err := bp.emit(ctx, ev, entry); err != nil {
			return err
		}
	}

	// Non-fatal: on failure the attempt simply re-evaluates on replay, which
	// matches the previous behavior (emit/disable are idempotent-by-design).
	if eval.Commit != nil {
		if err := eval.Commit(ctx); err != nil {
			bp.logger.Ctx(ctx).Warn("failed to mark attempt evaluated",
				zap.Error(err),
				zap.String("attempt_id", entry.Attempt.ID),
				zap.String("tenant_id", entry.Attempt.TenantID),
				zap.String("destination_id", entry.Destination.ID))
		}
	}
	return nil
}

// exhaustedRetriesKey is the per-(event,destination) suppression key for
// exhausted-retries alerts. Format is stable — changing it resets live windows.
func exhaustedRetriesKey(eventID, destinationID string) string {
	return "opevents:exhausted:" + eventID + ":" + destinationID
}

// emit delivers a single operator event and audits the send.
func (bp *BatchProcessor) emit(ctx context.Context, ev opevents.Event, entry *models.LogEntry) error {
	if err := bp.emitter.Emit(ctx, ev); err != nil {
		return err
	}
	bp.logger.Ctx(ctx).Audit("opevent delivered",
		zap.String("topic", ev.Topic),
		zap.String("attempt_id", entry.Attempt.ID),
		zap.String("event_id", entry.Event.ID),
		zap.String("tenant_id", ev.TenantID),
		zap.String("destination_id", entry.Destination.ID),
		zap.String("destination_type", entry.Destination.Type))
	return nil
}

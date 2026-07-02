package logmq

import (
	"context"
	"errors"
	"fmt"
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
	// ProcessedIdemp is the per-attempt replay gate: a failed attempt's
	// evaluate+deliver runs at most once per attempt ID within the gate's TTL.
	// Required when Evaluator is set.
	ProcessedIdemp idempotence.Idempotence
	// ExhaustedIdemp is the per-(event,destination) suppression window for
	// exhausted-retries alerts. Nil means no suppression (alert on every
	// exhaustion).
	ExhaustedIdemp idempotence.Idempotence
}

// BatchProcessorConfig configures the batch processor.
type BatchProcessorConfig struct {
	ItemCountThreshold int
	DelayThreshold     time.Duration
}

// BatchProcessor batches log entries and writes them to the log store.
type BatchProcessor struct {
	ctx      context.Context
	logger   *logging.Logger
	logStore LogStore
	alerts   AlertPipeline
	batcher  *batcher.Batcher[*mqs.Message]
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
		ctx:      ctx,
		logger:   logger,
		logStore: logStore,
		alerts:   alerts,
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

		if err := bp.processAlerts(bp.ctx, entry); err != nil {
			logger.Error("alert processing failed",
				zap.Error(err),
				zap.String("attempt_id", entry.Attempt.ID),
				zap.String("event_id", entry.Event.ID),
				zap.String("destination_id", entry.Destination.ID))
			// Nack so the message is redelivered. InsertMany is idempotent
			// (upsert by attempt ID) and the processed gate clears on failure,
			// so redelivery re-evaluates and re-emits any events not yet sent.
			validMsgs[i].Nack()
			continue
		}

		validMsgs[i].Ack()
	}
}

// processAlerts runs the alert pipeline for one persisted entry: evaluate the
// attempt, then act on the verdict (disable + opevents).
//
// A failed attempt is wrapped in the per-attempt processed gate, so a replay
// (MQ redelivery, producer re-publish) of a fully processed attempt is skipped
// instead of re-alerting. The gate marks processed only when evaluate+deliver
// complete, and clears on failure — so a nacked attempt re-runs in full on
// redelivery (counting stays correct: the store is idempotent per attempt ID).
// A success just resets the tracker — idempotent, so it needs no gate (and
// gating it would cost one Redis key per successful attempt).
func (bp *BatchProcessor) processAlerts(ctx context.Context, entry *models.LogEntry) error {
	attempt := alert.Attempt{
		TenantID:         entry.Destination.TenantID,
		DestinationID:    entry.Destination.ID,
		AttemptID:        entry.Attempt.ID,
		Number:           entry.Attempt.AttemptNumber,
		Success:          entry.Attempt.Status == models.AttemptStatusSuccess,
		EligibleForRetry: entry.Event.EligibleForRetry,
	}

	if attempt.Success {
		_, err := bp.alerts.Evaluator.Evaluate(ctx, attempt)
		return err
	}

	return bp.alerts.ProcessedIdemp.Exec(ctx, processedKey(attempt.AttemptID), func(ctx context.Context) error {
		eval, err := bp.alerts.Evaluator.Evaluate(ctx, attempt)
		if err != nil {
			return err
		}
		return bp.deliver(ctx, eval, entry)
	})
}

// deliver acts on an evaluation: disables the destination at the 100%
// threshold (when auto-disable is on), then emits the corresponding operator
// events in order — disabled, consecutive_failure, exhausted_retries.
// Exhausted-retries alerts are suppressed per (event, destination) within the
// configured window; a suppressed duplicate is treated as delivered.
func (bp *BatchProcessor) deliver(ctx context.Context, eval alert.Evaluation, entry *models.LogEntry) error {
	if eval.ConsecutiveFailure == nil && !eval.RetriesExhausted {
		return nil
	}

	dest := opevents.NewAlertDestination(entry.Destination)

	if cf := eval.ConsecutiveFailure; cf != nil {
		if cf.Level == 100 && bp.alerts.Disabler != nil {
			// Disable is idempotent on replay: a no-op if already disabled.
			if err := bp.alerts.Disabler.DisableDestination(ctx, dest.TenantID, dest.ID); err != nil {
				return fmt.Errorf("failed to disable destination: %w", err)
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

			if err := bp.emit(ctx, opevents.DestinationDisabledEvent(dest, entry.Event, entry.Attempt, now), entry); err != nil {
				return err
			}
		}

		ev := opevents.ConsecutiveFailureEvent(dest, entry.Event, entry.Attempt,
			cf.Failures, cf.Max, cf.Level)
		if err := bp.emit(ctx, ev, entry); err != nil {
			return err
		}
	}

	if eval.RetriesExhausted {
		ev := opevents.ExhaustedRetriesEvent(dest, entry.Event, entry.Attempt)
		if bp.alerts.ExhaustedIdemp != nil {
			return bp.alerts.ExhaustedIdemp.Exec(ctx, exhaustedRetriesKey(entry.Event.ID, dest.ID), func(ctx context.Context) error {
				return bp.emit(ctx, ev, entry)
			})
		}
		return bp.emit(ctx, ev, entry)
	}

	return nil
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

// emit delivers a single operator event and audits the send.
func (bp *BatchProcessor) emit(ctx context.Context, ev opevents.Event, entry *models.LogEntry) error {
	if err := bp.alerts.Emitter.Emit(ctx, ev); err != nil {
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

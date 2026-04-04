package logmq

import (
	"context"
	"errors"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
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

// AlertMonitor evaluates delivery attempts for alert conditions.
type AlertMonitor interface {
	HandleAttempt(ctx context.Context, attempt alert.DeliveryAttempt) error
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
	batcher      *batcher.Batcher[*mqs.Message]
}

// NewBatchProcessor creates a new batch processor for log entries.
func NewBatchProcessor(ctx context.Context, logger *logging.Logger, logStore LogStore, alertMonitor AlertMonitor, cfg BatchProcessorConfig) (*BatchProcessor, error) {
	bp := &BatchProcessor{
		ctx:          ctx,
		logger:       logger,
		logStore:     logStore,
		alertMonitor: alertMonitor,
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
	logger.Info("processing batch", zap.Int("message_count", len(msgs)))

	entries := make([]*models.LogEntry, 0, len(msgs))
	validMsgs := make([]*mqs.Message, 0, len(msgs))

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

		logger.Info("added to batch",
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

	// Per-entry alert evaluation after successful persistence
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
		if err := bp.alertMonitor.HandleAttempt(bp.ctx, da); err != nil {
			logger.Error("alert evaluation failed",
				zap.Error(err),
				zap.String("attempt_id", entry.Attempt.ID),
				zap.String("event_id", entry.Event.ID),
				zap.String("destination_id", entry.Destination.ID))
			validMsgs[i].Nack()
			continue
		}

		validMsgs[i].Ack()
	}
}

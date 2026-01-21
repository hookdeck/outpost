package logmq

import (
	"context"
	"errors"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/mikestefanello/batcher"
	"go.uber.org/zap"
)

// ErrInvalidLogEntry is returned when a LogEntry is missing required fields.
var ErrInvalidLogEntry = errors.New("invalid log entry: both event and delivery are required")

// LogStore defines the interface for persisting log entries.
// This is a consumer-defined interface containing only what logmq needs.
type LogStore interface {
	InsertMany(ctx context.Context, events []*models.Event, deliveries []*models.Delivery) error
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
	batcher  *batcher.Batcher[*mqs.Message]
}

// NewBatchProcessor creates a new batch processor for log entries.
func NewBatchProcessor(ctx context.Context, logger *logging.Logger, logStore LogStore, cfg BatchProcessorConfig) (*BatchProcessor, error) {
	bp := &BatchProcessor{
		ctx:      ctx,
		logger:   logger,
		logStore: logStore,
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

	events := make([]*models.Event, 0, len(msgs))
	deliveries := make([]*models.Delivery, 0, len(msgs))
	validMsgs := make([]*mqs.Message, 0, len(msgs))

	for _, msg := range msgs {
		entry := models.LogEntry{}
		if err := entry.FromMessage(msg); err != nil {
			logger.Error("failed to parse log entry",
				zap.Error(err),
				zap.String("message_id", msg.LoggableID))
			msg.Nack()
			continue
		}

		// Validate that both Event and Delivery are present.
		// The logstore requires both for data consistency.
		if entry.Event == nil || entry.Delivery == nil {
			logger.Error("invalid log entry: both event and delivery are required",
				zap.Bool("has_event", entry.Event != nil),
				zap.Bool("has_delivery", entry.Delivery != nil),
				zap.String("message_id", msg.LoggableID))
			msg.Nack()
			continue
		}

		events = append(events, entry.Event)
		deliveries = append(deliveries, entry.Delivery)
		validMsgs = append(validMsgs, msg)
	}

	// Nothing valid to insert
	if len(events) == 0 {
		return
	}

	if err := bp.logStore.InsertMany(bp.ctx, events, deliveries); err != nil {
		logger.Error("failed to insert events/deliveries",
			zap.Error(err),
			zap.Int("event_count", len(events)),
			zap.Int("delivery_count", len(deliveries)))
		for _, msg := range validMsgs {
			msg.Nack()
		}
		return
	}

	logger.Info("batch processed successfully", zap.Int("count", len(validMsgs)))

	for _, msg := range validMsgs {
		msg.Ack()
	}
}

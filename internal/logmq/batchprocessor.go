package logmq

import (
	"context"
	"errors"
	"sync"
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
	// ProcessedIdemp is the per-attempt replay gate: a replay of a fully
	// processed failed attempt is skipped instead of re-counting/re-alerting.
	// Used split-phase (Processed before eval, MarkProcessed after delivery) —
	// no in-flight conflict detection, so concurrent duplicates both run and
	// may both emit (tolerated: opevents are at-least-once). Required when
	// Evaluator is set.
	ProcessedIdemp idempotence.Idempotence
	// ExhaustedIdemp is the per-(event,destination) suppression window for
	// exhausted-retries alerts. Nil means no suppression (alert on every
	// exhaustion).
	ExhaustedIdemp idempotence.Idempotence
}

// BatchProcessorConfig configures the batch processor. The pool fields are
// test-only overrides: zero means "derive from ItemCountThreshold" (see the
// derive* functions), which is how production always runs — no extra knobs.
type BatchProcessorConfig struct {
	ItemCountThreshold int
	DelayThreshold     time.Duration
	// PostprocessShards is the postprocess pool's shard count — the eval
	// parallelism across destinations (same destination always evaluates
	// serially). Zero derives from ItemCountThreshold.
	PostprocessShards int
	// PostprocessShardQueueDepth bounds each shard's queue; a full shard
	// blocks the batch loop (backpressure). Zero derives.
	PostprocessShardQueueDepth int
	// DeliveryConcurrency is the opevent delivery pool's worker count.
	// Zero derives from ItemCountThreshold.
	DeliveryConcurrency int
	// DeliveryQueueDepth bounds the delivery queue; a full queue blocks the
	// postprocess pool's workers (backpressure). Zero means 2× concurrency.
	DeliveryQueueDepth int
}

// Pool sizing derives from ItemCountThreshold (LOG_BATCH_SIZE), which doubles
// as the operator's throughput declaration: batches are sized to roughly one
// second of traffic, so LogBatchSize ≈ entries/second. The heuristic is wrong
// only in the cheap direction — an oversized batch buys idle shards (parked
// goroutines) and deeper-but-bounded queues.
//
// The safety bound behind all of it: a fetched message must reach its
// terminal state within the broker's visibility window (~60s), or it
// redelivers while still held. In-flight is bounded (a full queue blocks its
// producer, all the way back to the consumer fetch), so worst-case ack
// latency is
//
//	held/drain ≈ (2×batch + 3×workers) / (workers/emitTimeout)
//
// which the derived values keep under the window with margin (~17s vs ~60s
// until the worker cap binds; see sizing_test.go) even with every send at
// emitTimeout.
const (
	// evalsPerShardPerSec is the eval throughput one shard sustains: an eval
	// is ~2ms of Redis ops (≈500/s), halved for headroom because the disable
	// path also writes the DB inside the ordered lane.
	evalsPerShardPerSec = 250
	// minPostprocessShards keeps small deployments' eval lane comfortably
	// ahead of intake; a shard costs one parked goroutine + one channel.
	minPostprocessShards = 8
	// maxPostprocessShards marks where shard count stops being the
	// bottleneck: 64 shards claim >16k evals/s through one Redis and one
	// intake consumer — horizontal-scale territory.
	maxPostprocessShards = 64

	minDeliveryConcurrency = 10
	// maxDeliveryConcurrency caps the derived worker count (reached at
	// LOG_BATCH_SIZE ≈ 1640). With the cap, a timeout-pinned sink stays
	// visibility-safe up to LOG_BATCH_SIZE ≈ 37k (see sizing_test.go);
	// deployments past that scale horizontally.
	maxDeliveryConcurrency = 8192
)

// derivePostprocessShards sizes eval parallelism to intake: shards ×
// evalsPerShardPerSec must cover LogBatchSize entries/second, so the ordered
// lane is never the pipeline's bottleneck.
func derivePostprocessShards(batchSize int) int {
	return clamp(batchSize/evalsPerShardPerSec, minPostprocessShards, maxPostprocessShards)
}

// derivePostprocessShardQueueDepth spreads one full batch across the shard
// queues (depth × shards ≈ LogBatchSize): dispatching a batch never blocks
// while the pipeline is healthy, so batch N+1's persist never waits on a
// merely-busy (vs stuck) stage. A stuck shard still backpressures — depth is
// finite.
func derivePostprocessShardQueueDepth(batchSize, shards int) int {
	return max(1, (batchSize+shards-1)/shards)
}

// deriveDeliveryConcurrency sizes the pool for full line rate at the worst
// send latency the emit timeout accepts: workers = rate × latency =
// LogBatchSize/s × emitTimeout. A sink may run at the timeout's edge
// indefinitely without the pipeline falling behind; past the timeout, sends
// fail into nack/redelivery, which no worker count can (or should) mask.
// Events-per-attempt doesn't factor in: a worker fans an attempt's sends out
// concurrently, so it is occupied ~one send latency regardless of K.
func deriveDeliveryConcurrency(batchSize int) int {
	return clamp(batchSize*int(emitTimeout/time.Second), minDeliveryConcurrency, maxDeliveryConcurrency)
}

func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

// BatchProcessor batches log entries and writes them to the log store.
type BatchProcessor struct {
	ctx      context.Context
	logger   *logging.Logger
	logStore LogStore
	alerts   AlertPipeline
	batcher  *batcher.Batcher[*mqs.Message]
	// Both pools are nil in persist-only mode (no Evaluator — the pipeline
	// then just inserts and acks; production always wires an Evaluator).
	postprocessPool *postprocessPool // stage 1: ordered eval + disable + plan
	deliveryPool    *deliveryPool    // stage 2: unordered opevent delivery
	shutdownOnce    sync.Once
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

	if alerts.Evaluator != nil {
		concurrency := cfg.DeliveryConcurrency
		if concurrency <= 0 {
			concurrency = deriveDeliveryConcurrency(cfg.ItemCountThreshold)
		}
		queueDepth := cfg.DeliveryQueueDepth
		if queueDepth <= 0 {
			queueDepth = 2 * concurrency
		}
		bp.deliveryPool = newDeliveryPool(ctx, logger, alerts, concurrency, queueDepth)

		shards := cfg.PostprocessShards
		if shards <= 0 {
			shards = derivePostprocessShards(cfg.ItemCountThreshold)
		}
		shardDepth := cfg.PostprocessShardQueueDepth
		if shardDepth <= 0 {
			shardDepth = derivePostprocessShardQueueDepth(cfg.ItemCountThreshold, shards)
		}
		bp.postprocessPool = newPostprocessPool(ctx, logger, alerts, bp.deliveryPool, shards, shardDepth)
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

// Shutdown gracefully shuts down the batch processor, upstream first so each
// stage drains with no concurrent producers: the batcher (flushes pending
// batches, which may still dispatch), then the postprocess pool (draining workers
// may still enqueue deliveries), then the delivery pool. Every in-flight
// message reaches a terminal state before Shutdown returns. Idempotent.
func (bp *BatchProcessor) Shutdown() {
	bp.shutdownOnce.Do(func() {
		bp.batcher.Shutdown()
		if bp.postprocessPool != nil {
			bp.postprocessPool.shutdown()
		}
		if bp.deliveryPool != nil {
			bp.deliveryPool.shutdown()
		}
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

	// Hand each persisted entry to the postprocess pool — enqueue-and-return
	// (unless its shard queue is full), so persistence throughput never waits
	// on eval or delivery.
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

		bp.postprocessPool.dispatch(entry, validMsgs[i])
	}
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

package logmq

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/opevents"
	"go.uber.org/zap"
)

// postprocessTask is one persisted entry awaiting alert evaluation, plus the message
// whose terminal state the pipeline decides.
type postprocessTask struct {
	entry *models.LogEntry
	msg   *mqs.Message
}

// postprocessPool runs the ordered half of the alert pipeline — evaluate, disable,
// plan — off the batch loop, so a slow eval never blocks persistence of the
// next batch. Tasks are sharded by destination (hash(destID) % N): one worker
// owns each shard's FIFO, which keeps per-destination eval order (and thus
// failure counting) exactly as serial processing had it, while different
// destinations evaluate concurrently. Delivery of the planned events is stage
// 2 (deliveryPool).
type postprocessPool struct {
	ctx    context.Context
	logger *logging.Logger
	alerts AlertPipeline
	pool   *deliveryPool

	shards []chan postprocessTask
	wg     sync.WaitGroup
}

func newPostprocessPool(ctx context.Context, logger *logging.Logger, alerts AlertPipeline, pool *deliveryPool, shardCount, queueDepth int) *postprocessPool {
	ap := &postprocessPool{
		ctx:    ctx,
		logger: logger,
		alerts: alerts,
		pool:   pool,
		shards: make([]chan postprocessTask, shardCount),
	}
	ap.wg.Add(shardCount)
	for i := range ap.shards {
		ap.shards[i] = make(chan postprocessTask, queueDepth)
		go ap.worker(ap.shards[i])
	}
	return ap
}

// dispatch routes the entry to its destination's shard. It blocks while that
// shard's queue is full — the backpressure path: it stalls the batch loop,
// which stalls the batcher, which stops draining the broker. A full shard
// head-of-line blocks the dispatch of other shards' entries in the same batch;
// accepted for simplicity (the queues drain in Redis time, ~ms).
func (ap *postprocessPool) dispatch(entry *models.LogEntry, msg *mqs.Message) {
	ap.shards[shardIndex(entry.Destination.ID, len(ap.shards))] <- postprocessTask{entry: entry, msg: msg}
}

// shardIndex maps a destination to its shard. Same destination → same shard →
// serial eval; the mapping is stable for the process lifetime.
func shardIndex(destinationID string, shardCount int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(destinationID))
	return int(h.Sum32() % uint32(shardCount))
}

// shutdown stops intake and drains every shard: each queued task reaches eval
// (and, when it owes events, the delivery queue) before shutdown returns. The
// caller must guarantee no concurrent dispatches (the batcher is shut down
// first) and must shut the delivery pool down after (draining workers still
// enqueue into it).
func (ap *postprocessPool) shutdown() {
	for _, shard := range ap.shards {
		close(shard)
	}
	ap.wg.Wait()
}

func (ap *postprocessPool) worker(shard chan postprocessTask) {
	defer ap.wg.Done()
	for task := range shard {
		ap.evalAndDispatch(ap.ctx, task.entry, task.msg)
	}
}

// evalAndDispatch runs the alert pipeline for one persisted entry and owns the
// message's terminal state: evaluate the attempt, act on the verdict (disable,
// plan the operator events), then hand delivery to the pool.
//
// A failed attempt runs inside the per-attempt processed gate, so a replay
// (MQ redelivery, producer re-publish) of a fully processed attempt is skipped
// instead of re-counting or re-alerting. The check runs BEFORE eval — a stale
// replay arriving after a success reset must not count toward the fresh
// streak. The mark lands only after the attempt's events are delivered (in the
// pool worker; inline here when there are none) — a nacked attempt re-runs in
// full on redelivery (counting stays correct: the store is idempotent per
// attempt ID). A success just resets the tracker — idempotent, so it needs no
// gate (and gating it would cost one Redis key per successful attempt).
func (ap *postprocessPool) evalAndDispatch(ctx context.Context, entry *models.LogEntry, msg *mqs.Message) {
	attempt := alert.Attempt{
		TenantID:         entry.Destination.TenantID,
		DestinationID:    entry.Destination.ID,
		AttemptID:        entry.Attempt.ID,
		Number:           entry.Attempt.AttemptNumber,
		Success:          entry.Attempt.Status == models.AttemptStatusSuccess,
		EligibleForRetry: entry.Event.EligibleForRetry,
	}

	if attempt.Success {
		if _, err := ap.alerts.Evaluator.Evaluate(ctx, attempt); err != nil {
			ap.nackAlertFailure(ctx, err, entry, msg)
			return
		}
		msg.Ack()
		return
	}

	key := processedKey(attempt.AttemptID)
	processed, err := ap.alerts.ProcessedIdemp.Processed(ctx, key)
	if err != nil {
		ap.nackAlertFailure(ctx, err, entry, msg)
		return
	}
	if processed {
		msg.Ack()
		return
	}

	eval, err := ap.alerts.Evaluator.Evaluate(ctx, attempt)
	if err != nil {
		ap.nackAlertFailure(ctx, err, entry, msg)
		return
	}

	events, err := ap.plan(ctx, eval, entry)
	if err != nil {
		ap.nackAlertFailure(ctx, err, entry, msg)
		return
	}

	// Common case: nothing to deliver — the attempt is fully processed here.
	if len(events) == 0 {
		if err := ap.alerts.ProcessedIdemp.MarkProcessed(ctx, key); err != nil {
			ap.nackAlertFailure(ctx, err, entry, msg)
			return
		}
		msg.Ack()
		return
	}

	// Blocks while the delivery queue is full (backpressure). The worker owns
	// the message's terminal state from here.
	ap.pool.enqueue(delivery{
		events:       events,
		entry:        entry,
		msg:          msg,
		processedKey: key,
	})
}

// nackAlertFailure logs an alert-pipeline failure and nacks. InsertMany is
// idempotent (upsert by attempt ID) and a failed attempt is never marked
// processed, so redelivery re-evaluates and re-emits — events already sent may
// go out again (at-least-once).
func (ap *postprocessPool) nackAlertFailure(ctx context.Context, err error, entry *models.LogEntry, msg *mqs.Message) {
	ap.logger.Ctx(ctx).Error("alert processing failed",
		zap.Error(err),
		zap.String("attempt_id", entry.Attempt.ID),
		zap.String("event_id", entry.Event.ID),
		zap.String("destination_id", entry.Destination.ID))
	msg.Nack()
}

// plan acts on an evaluation and builds the operator events owed for this
// attempt — disabled, consecutive_failure, exhausted_retries. The delivery
// worker sends them concurrently, so slice order carries no meaning.
// The disable (a DB write) happens here, in the ordered
// lane: it's an action, not a notification, and it must precede event
// construction so the payloads carry the destination's latest state
// (disabled). The events are complete at return — workers share no mutable
// state with the eval side.
func (ap *postprocessPool) plan(ctx context.Context, eval alert.Evaluation, entry *models.LogEntry) ([]deliveryEvent, error) {
	if eval.ConsecutiveFailure == nil && !eval.RetriesExhausted {
		return nil, nil
	}

	dest := opevents.NewAlertDestination(entry.Destination)
	var events []deliveryEvent

	if cf := eval.ConsecutiveFailure; cf != nil {
		if cf.Level == 100 && ap.alerts.Disabler != nil {
			// Disable converges on replay: re-disabling rewrites DisabledAt,
			// but the end state is the same.
			if err := ap.alerts.Disabler.DisableDestination(ctx, dest.TenantID, dest.ID); err != nil {
				return nil, fmt.Errorf("failed to disable destination: %w", err)
			}

			// The payload carries the destination's latest state: disabled.
			now := time.Now()
			dest.DisabledAt = &now

			ap.logger.Ctx(ctx).Audit("destination disabled",
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
		if ap.alerts.ExhaustedIdemp != nil {
			de.suppressKey = exhaustedRetriesKey(entry.Event.ID, dest.ID)
		}
		events = append(events, de)
	}

	return events, nil
}

package logmq_test

// Replay / idempotency. Model C holds the logmq message un-acked until delivery
// completes and relies on redelivery for durability, so the same attempt is
// legitimately processed more than once. These tests pin that a replay does not
// double-count, double-alert, or double-persist.

import (
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/require"
)

// Same failed attempt ID delivered twice (second in a later batch) → count
// incremented once, exactly ONE alert record total, both messages acked,
// persisted once.
func TestCharacterization_ReplaySameAttempt(t *testing.T) {
	t.Parallel()
	// max=2, thresholds[50,100] → count 1 = 50% (single cf alert, no disable).
	// itemCountThreshold=1 → each Add is its own batch (so the replay takes the
	// cross-batch path, not in-batch dedup).
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert: alertConfig{
			autoDisableCount: 2,
			thresholds:       []int{50, 100},
		},
	})

	destA, tenant := "dest_b1", "tenant_b1"
	entry := makeEntry(destA, tenant, "att_replay", models.AttemptStatusFailed)

	cm1, msg1 := newCountingMessage(entry)
	h.add(msg1)
	h.waitTerminal([]*countingMessage{cm1})

	cm2, msg2 := newCountingMessage(entry)
	h.add(msg2)
	h.waitTerminal([]*countingMessage{cm2})

	recs := h.sink.forDest(destA)
	require.Equal(t, []string{topicCF}, topics(recs), "exactly one alert across both deliveries")

	cm1.requireAcked(t)
	cm2.requireAcked(t)

	// Persisted once (idempotent upsert by attempt ID).
	require.Len(t, h.listAttempt(destA), 1)
}

// retryMaxLimit=3; dest A attempt EligibleForRetry=true, AttemptNumber=4 →
// exactly one exhausted_retries record; acked. Run WITHOUT the idempotence window
// (single pipeline-level check; negatives live in monitor_test.go).
func TestCharacterization_ExhaustedRetries(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert: alertConfig{
			retryMaxLimit: 3,
			// High auto-disable count so the cf threshold (count 1) never fires here.
			autoDisableCount: 100,
		},
	})

	destA, tenant := "dest_b3", "tenant_b3"
	entry := makeEntryFull(destA, tenant, "att_exhaust", models.AttemptStatusFailed, 4, true)

	cm, msg := newCountingMessage(entry)
	h.add(msg)
	h.waitTerminal([]*countingMessage{cm})

	recs := h.sink.forDest(destA)
	require.Equal(t, []string{topicExhaust}, topics(recs))

	cm.requireAcked(t)
}

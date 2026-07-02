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

// A stale replay of an old failed attempt arriving AFTER a success reset is
// skipped by the per-attempt processed gate — it must not re-count toward the
// fresh streak. (Pre-step-3 behavior: the success reset also cleared the
// replay markers, so the stale replay re-counted and could re-alert. The
// delivery-owned gate survives the reset; this pins the intended new behavior.)
func TestCharacterization_StaleReplayAfterReset_Skipped(t *testing.T) {
	t.Parallel()
	// max=2, thresholds[100] → an alert fires only at count 2. If the stale
	// replay re-counted, att_fresh would be count 2 → alert. It must not.
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert: alertConfig{
			autoDisableCount: 2,
			thresholds:       []int{100},
		},
	})

	destA, tenant := "dest_sr", "tenant_sr"
	stale := makeEntry(destA, tenant, "att_stale", models.AttemptStatusFailed)

	// Fail (count 1, below threshold), then success (reset).
	cm1, msg1 := newCountingMessage(stale)
	h.add(msg1)
	h.waitTerminal([]*countingMessage{cm1})

	cm2, msg2 := newCountingMessage(makeEntry(destA, tenant, "att_ok", models.AttemptStatusSuccess))
	h.add(msg2)
	h.waitTerminal([]*countingMessage{cm2})

	// Stale replay of the old failed attempt: skipped by the processed gate.
	cm3, msg3 := newCountingMessage(stale)
	h.add(msg3)
	h.waitTerminal([]*countingMessage{cm3})

	// A fresh failure starts a new streak at count 1 — no alert.
	cm4, msg4 := newCountingMessage(makeEntry(destA, tenant, "att_fresh", models.AttemptStatusFailed))
	h.add(msg4)
	h.waitTerminal([]*countingMessage{cm4})

	require.Empty(t, h.sink.forDest(destA), "stale replay must not count toward the fresh streak")
	cm3.requireAcked(t)
	cm4.requireAcked(t)
}

// retryMaxLimit=3; dest A attempt EligibleForRetry=true, AttemptNumber=4 →
// exactly one exhausted_retries record; acked. Run WITHOUT the idempotence window
// (single pipeline-level check; negatives live in the alert package tests).
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

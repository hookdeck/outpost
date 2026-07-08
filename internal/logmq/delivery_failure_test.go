package logmq_test

// Delivery failure paths: the emit timeout, a replay-gate mark failure, and a
// partial multi-event failure. All three must end in a nack with the gate
// unmarked, so redelivery re-runs the attempt in full (at-least-once).

import (
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
)

// A send slower than the emit timeout fails the delivery: the message nacks,
// nothing is recorded, and the gate stays unmarked. The timeout is what bounds
// an entry goroutine's lifetime, so this is the path a stuck sink takes.
func TestDelivery_EmitTimeout_Nacks(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1, emitTimeout: 50 * time.Millisecond},
		// max=2, thresholds[50,100] → a single failure (count 1 = 50%) alerts.
		alert: alertConfig{autoDisableCount: 2, thresholds: []int{50, 100}},
		doubles: doublesConfig{
			// The blocked send is never released before the timeout; the
			// ctx-aware sink returns ctx.Err() when the 50ms deadline fires.
			sinkBlockOn: map[string]bool{"att_timeout": true},
		},
	})

	dest, tenant := "dest_to", "tenant_to"
	cm, msg := newCountingMessage(makeEntry(dest, tenant, "att_timeout", models.AttemptStatusFailed))
	h.add(msg)
	h.waitTerminal([]*countingMessage{cm})

	cm.requireNacked(t)
	assert.Empty(t, h.sink.forDest(dest), "the timed-out send delivered nothing")
}

// All sends succeed but marking the replay gate fails (e.g. Redis error after
// delivery): the message nacks. The event went out — redelivery re-runs the
// attempt and may re-send it (tolerated duplicate), which is why the mark must
// come before the ack, not after.
func TestDelivery_MarkProcessedFailure_Nacks(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{autoDisableCount: 2, thresholds: []int{50, 100}},
		doubles: doublesConfig{failMarkProcessed: true},
	})

	dest, tenant := "dest_mf", "tenant_mf"
	cm, msg := newCountingMessage(makeEntry(dest, tenant, "att_markfail", models.AttemptStatusFailed))
	h.add(msg)
	h.waitTerminal([]*countingMessage{cm})

	cm.requireNacked(t)
	assert.ElementsMatch(t, []string{topicFailed, topicCF}, topics(h.sink.forDest(dest)),
		"the events were delivered before the mark failed")
}

// An attempt owing two events (disabled + cf at the 100% threshold) where one
// send fails: the whole attempt nacks with the gate unmarked. The surviving
// sibling may or may not have gone out (concurrent sends) — redelivery re-runs
// both, so the failure mode is a duplicate, never a loss.
func TestDelivery_PartialSendFailure_Nacks(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		// max=1 → the first failure is the 100% threshold: disabled + cf.
		alert: alertConfig{autoDisableCount: 1, thresholds: []int{100}, withDisabler: true},
		doubles: doublesConfig{
			sinkFailOn: map[string]bool{topicCF: true}, // cf fails, disabled succeeds
		},
	})

	dest, tenant := "dest_pf", "tenant_pf"
	cm, msg := newCountingMessage(makeEntry(dest, tenant, "att_partial", models.AttemptStatusFailed))
	h.add(msg)
	h.waitTerminal([]*countingMessage{cm})

	cm.requireNacked(t)
	assert.NotContains(t, topics(h.sink.forDest(dest)), topicCF,
		"the failed send recorded nothing")
}

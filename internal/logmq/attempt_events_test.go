package logmq_test

// attempt.success / attempt.failed emission. The failed side is pinned all
// over the characterization suite (every failed attempt's expected multiset
// carries one attempt.failed); these cover the success side, which has its own
// path — no replay gate, no mark, send-then-ack.

import (
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
)

// A successful attempt emits exactly one attempt.success and acks.
func TestAttemptEvents_SuccessEmits(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{autoDisableCount: 2, thresholds: []int{50, 100}},
	})

	dest, tenant := "dest_ae1", "tenant_ae1"
	cm, msg := newCountingMessage(makeEntry(dest, tenant, "att_ok", models.AttemptStatusSuccess))
	h.add(msg)
	h.waitTerminal([]*countingMessage{cm})

	cm.requireAcked(t)
	recs := h.sink.forDest(dest)
	assert.Equal(t, []string{topicSuccess}, topics(recs))
	assert.Equal(t, []string{"att_ok"}, attemptIDs(recs))
}

// A failed attempt.success send nacks the message: the success path owes its
// event the same at-least-once treatment as the failure path (redelivery
// re-runs the reset — idempotent — and re-sends).
func TestAttemptEvents_SuccessEmitFailure_Nacks(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{autoDisableCount: 2, thresholds: []int{50, 100}},
		doubles: doublesConfig{
			sinkFailOn: map[string]bool{topicSuccess: true},
		},
	})

	dest, tenant := "dest_ae2", "tenant_ae2"
	cm, msg := newCountingMessage(makeEntry(dest, tenant, "att_ok", models.AttemptStatusSuccess))
	h.add(msg)
	h.waitTerminal([]*countingMessage{cm})

	cm.requireNacked(t)
	assert.Empty(t, h.sink.forDest(dest), "the failed send recorded nothing")
}

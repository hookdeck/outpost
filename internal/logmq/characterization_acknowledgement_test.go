package logmq_test

// Acknowledgement (exactly-once terminal state). Cross-cutting: every message
// ends in exactly one terminal state (ack XOR nack), the correct one per the
// routing table, and one message's failure must not disturb another's ack.

import (
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One batch with a success, a failure that alerts, an in-batch duplicate, an
// unparseable body, and an entry whose emit is injected to fail. Every message
// has exactly one terminal state (the correct one); the emit-fail nack does not
// disturb the others' acks.
func TestCharacterization_MixedBatchAccounting(t *testing.T) {
	t.Parallel()
	// 6 messages total: success, alert-failure, dup(x2), unparseable, emit-fail.
	// thresholds[50,100] with max=2 so a single failure (count 1) emits a cf alert.
	emitFailAttemptID := "att_emit_fail"
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 6},
		alert: alertConfig{
			autoDisableCount: 2,
			thresholds:       []int{50, 100},
		},
		doubles: doublesConfig{
			sinkFailOn: map[string]bool{emitFailAttemptID: true},
		},
	})

	tenant := "tenant_c1"
	destSuccess := "dest_c1_success"
	destAlert := "dest_c1_alert"
	destDup := "dest_c1_dup"
	destFail := "dest_c1_fail"

	// 1. success
	cmSuccess, msgSuccess := newCountingMessage(makeEntry(destSuccess, tenant, "att_success", models.AttemptStatusSuccess))
	// 2. failure that alerts (count 1 = 50%)
	cmAlert, msgAlert := newCountingMessage(makeEntry(destAlert, tenant, "att_alert", models.AttemptStatusFailed))
	// 3. in-batch duplicate (byte-identical, success → no alert)
	dupEntry := makeEntry(destDup, tenant, "att_dup", models.AttemptStatusSuccess)
	cmDupKeep, msgDupKeep := newCountingMessage(dupEntry)
	cmDupCopy, msgDupCopy := newCountingMessage(dupEntry)
	// 4. unparseable body
	cmInvalid, msgInvalid := newRawMessage([]byte("not valid json"))
	// 5. emit-fail entry (failure → cf emit injected to fail)
	cmEmitFail, msgEmitFail := newCountingMessage(makeEntry(destFail, tenant, emitFailAttemptID, models.AttemptStatusFailed))

	for _, msg := range []*mqs.Message{msgSuccess, msgAlert, msgDupKeep, msgDupCopy, msgInvalid, msgEmitFail} {
		h.add(msg)
	}
	all := []*countingMessage{cmSuccess, cmAlert, cmDupKeep, cmDupCopy, cmInvalid, cmEmitFail}
	h.waitTerminal(all)

	// Exactly one terminal state per message.
	for _, m := range all {
		m.requireTerminalOnce(t)
	}

	// Success / dup(both) / alert → ack.
	cmSuccess.requireAcked(t)
	cmAlert.requireAcked(t)
	cmDupKeep.requireAcked(t)
	cmDupCopy.requireAcked(t)
	// Invalid / emit-fail → nack.
	cmInvalid.requireNacked(t)
	cmEmitFail.requireNacked(t)

	// Only the alerting destination produced a (successful) record.
	assert.Equal(t, []string{topicCF}, topics(h.sink.forDest(destAlert)))
	assert.Empty(t, h.sink.forDest(destFail), "emit-fail destination produced no recorded event")
	assert.Len(t, h.sink.snapshot(), 1, "exactly one event delivered to the sink")
}

// A single failed attempt below any threshold (count 1) → persisted, acked, zero
// sink records. (Under Model C this becomes the "ack immediately, no delivery
// task" path; the assertion is identical.)
func TestCharacterization_BelowThresholdNoAlert(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{withDisabler: true},
	})

	destA, tenant := "dest_c2", "tenant_c2"
	cm, msg := newCountingMessage(makeEntry(destA, tenant, "att_1", models.AttemptStatusFailed))
	h.add(msg)
	h.waitTerminal([]*countingMessage{cm})

	cm.requireAcked(t)
	assert.Empty(t, h.sink.snapshot(), "count 1 is below the 50%% threshold (5)")
	require.Len(t, h.listAttempt(destA), 1, "attempt persisted")
}

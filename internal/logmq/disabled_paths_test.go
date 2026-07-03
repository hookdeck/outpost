package logmq_test

// Early-outs when alert signals and/or attempt opevents are configured off:
// per-entry cost should scale with what's enabled. With every signal off the
// failed path skips the replay gate (nothing to protect — attempt.failed is
// at-least-once like attempt.success); with attempt topics also unsubscribed
// the per-entry pipeline is skipped entirely.

import (
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
)

// With alert signals off, a failed attempt emits attempt.failed and acks —
// ungated, so a replay of the same attempt re-emits instead of being skipped
// (the gated path pins the opposite in ReplaySameAttempt). No alert topics
// fire and nothing is disabled, no matter how many failures accumulate.
func TestDisabledSignals_FailedEmitsUngated(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{autoDisableCount: 2, thresholds: []int{50, 100}, withDisabler: true, signalsOff: true},
	})

	dest, tenant := "dest_ds1", "tenant_ds1"
	cm1, msg1 := newCountingMessage(makeEntry(dest, tenant, "att_ds_1", models.AttemptStatusFailed))
	cm2, msg2 := newCountingMessage(makeEntry(dest, tenant, "att_ds_2", models.AttemptStatusFailed))
	cmOK, msgOK := newCountingMessage(makeEntry(dest, tenant, "att_ds_ok", models.AttemptStatusSuccess))
	// Paced: the replay below needs att_ds_1 terminal before its copy arrives.
	h.add(msg1)
	h.waitTerminal([]*countingMessage{cm1})
	cmReplay, msgReplay := newCountingMessage(makeEntry(dest, tenant, "att_ds_1", models.AttemptStatusFailed))
	h.add(msgReplay)
	h.add(msg2)
	h.add(msgOK)
	h.waitTerminal([]*countingMessage{cm2, cmOK, cmReplay})

	cm1.requireAcked(t)
	cm2.requireAcked(t)
	cmOK.requireAcked(t)
	cmReplay.requireAcked(t)

	recs := h.sink.forDest(dest)
	assert.ElementsMatch(t, []string{topicFailed, topicFailed, topicFailed, topicSuccess}, topics(recs),
		"every failed copy emits (no gate), success emits, no alert topics fire")
	assert.ElementsMatch(t, []string{"att_ds_1", "att_ds_1", "att_ds_2"}, attemptIDs(forTopic(recs, topicFailed)),
		"the replayed attempt emits twice — ungated")
	assert.Empty(t, h.disabler.snapshot(), "signals off never disables, past any threshold")
}

// With alert signals off, a failed attempt.failed send still nacks for
// redelivery — the early-out skips the gate, not the delivery guarantee.
func TestDisabledSignals_FailedEmitFailure_Nacks(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{signalsOff: true},
		doubles: doublesConfig{sinkFailOn: map[string]bool{topicFailed: true}},
	})

	dest, tenant := "dest_ds2", "tenant_ds2"
	cm, msg := newCountingMessage(makeEntry(dest, tenant, "att_ds_f", models.AttemptStatusFailed))
	h.add(msg)
	h.waitTerminal([]*countingMessage{cm})

	cm.requireNacked(t)
	assert.Empty(t, h.sink.forDest(dest))
}

// Signals off AND no attempt topic subscribed: the pipeline can't produce
// anything, so entries ack straight after persistence — no goroutine, no
// Redis, no sink traffic.
func TestDisabledSignals_NothingEnabled_AcksAfterPersist(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 2},
		alert:   alertConfig{signalsOff: true, opeventTopics: []string{topicCF}},
	})

	dest, tenant := "dest_ds3", "tenant_ds3"
	cmFail, msgFail := newCountingMessage(makeEntry(dest, tenant, "att_ds_n1", models.AttemptStatusFailed))
	cmOK, msgOK := newCountingMessage(makeEntry(dest, tenant, "att_ds_n2", models.AttemptStatusSuccess))
	h.add(msgFail)
	h.add(msgOK)
	h.waitTerminal([]*countingMessage{cmFail, cmOK})

	cmFail.requireAcked(t)
	cmOK.requireAcked(t)
	assert.Empty(t, h.sink.snapshot(), "nothing enabled, nothing emitted")
	assert.Len(t, h.listAttempt(dest), 2, "persistence is unaffected by the early-out")
}

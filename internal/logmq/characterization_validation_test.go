package logmq_test

// Intake / validation. The front of the pipeline: in-batch dedup and the
// nack-all-on-persist-failure path. Both are concurrency-relevant, so they are
// pinned here even though some intake routing is also covered by batchprocessor_test.go.

import (
	"fmt"
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two messages with identical Attempt.ID in one batch → persisted once
// (ListAttempt shows 1), both messages acked (the kept entry and the duplicate
// copy), total nack==0.
func TestCharacterization_InBatchDuplicate(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 2},
		alert:   alertConfig{withDisabler: true},
	})

	destA, tenant := "dest_04", "tenant_04"
	entry := makeEntry(destA, tenant, "att_dup", models.AttemptStatusSuccess)
	cm1, msg1 := newCountingMessage(entry)
	cm2, msg2 := newCountingMessage(entry)
	h.add(msg1)
	h.add(msg2)
	h.waitTerminal([]*countingMessage{cm1, cm2})

	require.Len(t, h.listAttempt(destA), 1, "duplicate persisted once")

	// Both the kept entry and the duplicate copy are acked; neither nacked.
	cm1.requireAcked(t)
	cm2.requireAcked(t)
}

// InsertMany returns error → all messages nacked, nothing persisted, zero sink
// records (eval never ran).
func TestCharacterization_InsertManyErrorNacksAll(t *testing.T) {
	t.Parallel()
	failing := &failingLogStore{err: fmt.Errorf("insert boom")}
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 3},
		alert:   alertConfig{withDisabler: true},
		doubles: doublesConfig{logStore: failing},
	})

	destA, tenant := "dest_05", "tenant_05"
	msgs := make([]*countingMessage, 0, 3)
	for i := 1; i <= 3; i++ {
		cm, msg := newCountingMessage(makeEntry(destA, tenant, fmt.Sprintf("att_%d", i), models.AttemptStatusFailed))
		msgs = append(msgs, cm)
		h.add(msg)
	}
	h.waitTerminal(msgs)

	for _, m := range msgs {
		m.requireNacked(t)
	}
	assert.Empty(t, h.sink.snapshot(), "no events emitted when persistence fails")
}

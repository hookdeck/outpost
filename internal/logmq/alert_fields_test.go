package logmq_test

import (
	"encoding/json"
	"testing"
	"testing/synctest"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A failed attempt flagged SkipFailureCount still emits attempt.failed, but
// stays out of the consecutive-failure count, so it never reaches auto-disable.
func TestSkipFailureCount_NotCountedNotDisabled(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, harnessConfig{
			batcher: batcherConfig{itemCount: 1},
			alert:   alertConfig{autoDisableCount: 2, thresholds: []int{100}, withDisabler: true},
		})

		dest, tenant := "dest_sfc", "tenant_sfc"
		var cms []*countingMessage
		for _, id := range []string{"att_sfc_1", "att_sfc_2", "att_sfc_3"} {
			entry := makeEntry(dest, tenant, id, models.AttemptStatusFailed)
			entry.SkipFailureCount = true
			cm, msg := newCountingMessage(entry)
			cms = append(cms, cm)
			h.add(msg)
		}
		h.waitTerminal(cms)

		for _, cm := range cms {
			cm.requireAcked(t)
		}
		recs := h.sink.forDest(dest)
		assert.Equal(t, []string{topicFailed, topicFailed, topicFailed}, topics(recs))
		assert.Empty(t, h.disabler.snapshot())
	})
}

func TestLogEntry_AlertFieldsJSON(t *testing.T) {
	t.Parallel()
	entry := makeEntry("dest_j", "tenant_j", "att_j", models.AttemptStatusFailed)

	body, err := json.Marshal(entry)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "attempt_max")
	assert.NotContains(t, string(body), "non_retryable")
	assert.NotContains(t, string(body), "skip_failure_count")

	entry.AttemptMax = 3
	entry.NonRetryable = true
	entry.SkipFailureCount = true
	body, err = json.Marshal(entry)
	require.NoError(t, err)
	var got models.LogEntry
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, 3, got.AttemptMax)
	assert.True(t, got.NonRetryable)
	assert.True(t, got.SkipFailureCount)
}

// A non-retryable failure of an eligible event raises exhausted_retries at its
// first attempt.
func TestNonRetryableFailure_RaisesExhaustedRetries(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, harnessConfig{
			batcher: batcherConfig{itemCount: 1},
			alert:   alertConfig{autoDisableCount: 100, thresholds: []int{100}},
		})

		dest, tenant := "dest_nrx", "tenant_nrx"
		plain, plainMsg := newCountingMessage(makeEntry(dest, tenant, "att_nrx_plain", models.AttemptStatusFailed))
		flagged := makeEntry(dest, tenant, "att_nrx_flagged", models.AttemptStatusFailed)
		flagged.NonRetryable = true
		cm, msg := newCountingMessage(flagged)
		h.add(plainMsg)
		h.add(msg)
		h.waitTerminal([]*countingMessage{plain, cm})

		recs := h.sink.forDest(dest)
		assert.Equal(t, []string{"att_nrx_flagged"}, attemptIDs(forTopic(recs, topicExhaust)))
	})
}

// AttemptMax replaces the evaluator's retry limit for the entry's attempt.
func TestAttemptMax_SetsExhaustedRetriesLimit(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t, harnessConfig{
			batcher: batcherConfig{itemCount: 1},
			alert:   alertConfig{autoDisableCount: 100, thresholds: []int{100}},
		})

		dest, tenant := "dest_amx", "tenant_amx"
		early := makeEntryFull(dest, tenant, "att_amx_early", models.AttemptStatusFailed, 2, true)
		early.AttemptMax = 3
		final := makeEntryFull(dest, tenant, "att_amx_final", models.AttemptStatusFailed, 3, true)
		final.AttemptMax = 3
		cmEarly, msgEarly := newCountingMessage(early)
		cmFinal, msgFinal := newCountingMessage(final)
		h.add(msgEarly)
		h.add(msgFinal)
		h.waitTerminal([]*countingMessage{cmEarly, cmFinal})

		recs := h.sink.forDest(dest)
		assert.Equal(t, []string{"att_amx_final"}, attemptIDs(forTopic(recs, topicExhaust)))
	})
}

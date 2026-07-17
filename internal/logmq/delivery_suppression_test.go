package logmq_test

// Delivery-layer exhausted-retries suppression: the delivery worker wraps the
// keyed exhausted event in an idempotence window. These tests exercise that
// path (the characterization suite wires no idempotence, so it doesn't cover
// it).

import (
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

// makeExhaustedEntry builds a failed attempt past the retry limit for a fixed
// tenant+destination, so its exhausted-retries suppression key is deterministic.
// cf alerting is kept quiet by the callers' high thresholds/auto-disable count.
func makeExhaustedEntry(destID, tenantID, eventID, attemptID string, attemptNumber int) models.LogEntry {
	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithID(eventID),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithEligibleForRetry(true),
		testutil.EventFactory.WithMatchedDestinationIDs([]string{destID}),
	)
	attempt := testutil.AttemptFactory.Any(
		testutil.AttemptFactory.WithID(attemptID),
		testutil.AttemptFactory.WithTenantID(tenantID),
		testutil.AttemptFactory.WithEventID(event.ID),
		testutil.AttemptFactory.WithDestinationID(destID),
		testutil.AttemptFactory.WithStatus(models.AttemptStatusFailed),
		testutil.AttemptFactory.WithAttemptNumber(attemptNumber),
	)
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID(destID),
		testutil.DestinationFactory.WithTenantID(tenantID),
	)
	return models.LogEntry{Event: &event, Attempt: &attempt, Destination: &dest}
}

// exhaustedAlertConfig keeps consecutive-failure alerting silent (threshold 100%,
// high auto-disable count) so only exhausted-retries events fire.
func exhaustedAlertConfig() alertConfig {
	return alertConfig{thresholds: []int{100}, autoDisableCount: 100, retryMaxLimit: 3}
}

// Two exhaustions on the same destination within the window → only the first
// delivers; the second is suppressed by the idempotence key. Same event here
// (the replay case); distinct events are covered by PerDestination.
func TestDelivery_ExhaustedRetries_WindowSuppression(t *testing.T) {
	t.Parallel()
	idemp := idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(10*time.Second))
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   exhaustedAlertConfig(),
		doubles: doublesConfig{idemp: idemp},
	})

	// Paced one at a time: concurrent Execs on the same window key would hit
	// the in-flight conflict path (sleep + ErrConflict) instead of the
	// suppression this test pins.
	dest, tenant, eventID := "dest_ws", "tenant_ws", "evt_ws"
	cm1, msg1 := newCountingMessage(makeExhaustedEntry(dest, tenant, eventID, "att_ws_1", 4))
	cm2, msg2 := newCountingMessage(makeExhaustedEntry(dest, tenant, eventID, "att_ws_2", 5))
	h.add(msg1)
	h.waitTerminal([]*countingMessage{cm1})
	h.add(msg2)
	h.waitTerminal([]*countingMessage{cm2})

	cm1.requireAcked(t)
	cm2.requireAcked(t)
	assert.ElementsMatch(t, []string{topicFailed, topicFailed, topicExhaust}, topics(h.sink.forDest(dest)),
		"second exhaustion on the same destination is suppressed within the window")
}

// With no suppression window (idemp nil == WindowSeconds 0), every exhaustion
// delivers — the key is present but there's no idempotence to enforce it.
func TestDelivery_ExhaustedRetries_NoWindowEmitsEvery(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 2},
		alert:   exhaustedAlertConfig(),
		// doubles.idemp left nil → no suppression window.
	})

	dest, tenant, eventID := "dest_nw", "tenant_nw", "evt_nw"
	cm1, msg1 := newCountingMessage(makeExhaustedEntry(dest, tenant, eventID, "att_nw_1", 4))
	cm2, msg2 := newCountingMessage(makeExhaustedEntry(dest, tenant, eventID, "att_nw_2", 5))
	h.add(msg1)
	h.add(msg2)
	h.waitTerminal([]*countingMessage{cm1, cm2})

	cm1.requireAcked(t)
	cm2.requireAcked(t)
	assert.ElementsMatch(t, []string{topicFailed, topicFailed, topicExhaust, topicExhaust}, topics(h.sink.forDest(dest)),
		"with no window, every exhaustion delivers")
}

// Distinct events exhausting on the same destination share the window key →
// at most one alert per destination within the window.
func TestDelivery_ExhaustedRetries_PerDestination(t *testing.T) {
	t.Parallel()
	idemp := idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(10*time.Second))
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   exhaustedAlertConfig(),
		doubles: doublesConfig{idemp: idemp},
	})

	// Paced one at a time: concurrent Execs on the shared window key would hit
	// the in-flight conflict path instead of the suppression this test pins.
	dest, tenant := "dest_pd", "tenant_pd"
	cm1, msg1 := newCountingMessage(makeExhaustedEntry(dest, tenant, "evt_pd_1", "att_pd_1", 4))
	cm2, msg2 := newCountingMessage(makeExhaustedEntry(dest, tenant, "evt_pd_2", "att_pd_2", 4))
	h.add(msg1)
	h.waitTerminal([]*countingMessage{cm1})
	h.add(msg2)
	h.waitTerminal([]*countingMessage{cm2})

	cm1.requireAcked(t)
	cm2.requireAcked(t)
	assert.ElementsMatch(t, []string{topicFailed, topicFailed, topicExhaust}, topics(h.sink.forDest(dest)),
		"a second event exhausting on the same destination within the window is suppressed")
}

// The window key is tenant-scoped: the same destination ID under two tenants
// gets independent windows, so one tenant's alert never suppresses another's.
func TestDelivery_ExhaustedRetries_TenantIsolation(t *testing.T) {
	t.Parallel()
	idemp := idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(10*time.Second))
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   exhaustedAlertConfig(),
		doubles: doublesConfig{idemp: idemp},
	})

	dest := "dest_ti"
	cm1, msg1 := newCountingMessage(makeExhaustedEntry(dest, "tenant_ti_a", "evt_ti_a", "att_ti_a", 4))
	cm2, msg2 := newCountingMessage(makeExhaustedEntry(dest, "tenant_ti_b", "evt_ti_b", "att_ti_b", 4))
	h.add(msg1)
	h.waitTerminal([]*countingMessage{cm1})
	h.add(msg2)
	h.waitTerminal([]*countingMessage{cm2})

	cm1.requireAcked(t)
	cm2.requireAcked(t)
	assert.ElementsMatch(t, []string{topicFailed, topicFailed, topicExhaust, topicExhaust}, topics(h.sink.forDest(dest)),
		"tenants with the same destination ID alert independently")
}

// Two events exhausting concurrently on one destination race on the shared
// window key: exactly one alert delivers, and the loser — whether it lands on
// the suppressed path or the in-flight conflict path — acks instead of nacking.
func TestDelivery_ExhaustedRetries_ConcurrentConflictAcks(t *testing.T) {
	t.Parallel()
	idemp := idempotence.New(testutil.CreateTestRedisClient(t),
		idempotence.WithSuccessfulTTL(10*time.Second),
		// Short conflict wait so the losing Exec resolves quickly.
		idempotence.WithTimeout(200*time.Millisecond),
	)
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 2},
		alert:   exhaustedAlertConfig(),
		doubles: doublesConfig{idemp: idemp},
	})

	dest, tenant := "dest_cc", "tenant_cc"
	cm1, msg1 := newCountingMessage(makeExhaustedEntry(dest, tenant, "evt_cc_1", "att_cc_1", 4))
	cm2, msg2 := newCountingMessage(makeExhaustedEntry(dest, tenant, "evt_cc_2", "att_cc_2", 4))
	h.add(msg1)
	h.add(msg2)
	h.waitTerminal([]*countingMessage{cm1, cm2})

	cm1.requireAcked(t)
	cm2.requireAcked(t)
	assert.ElementsMatch(t, []string{topicFailed, topicFailed, topicExhaust}, topics(h.sink.forDest(dest)),
		"concurrent exhaustions on one destination deliver exactly one alert")
}

// Emit failure clears the window key, so a later exhaustion on the same
// destination re-delivers instead of being suppressed.
func TestDelivery_ExhaustedRetries_EmitFailureClearsWindow(t *testing.T) {
	t.Parallel()
	idemp := idempotence.New(testutil.CreateTestRedisClient(t), idempotence.WithSuccessfulTTL(10*time.Second))
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   exhaustedAlertConfig(),
		doubles: doublesConfig{
			idemp: idemp,
			// Only the exhausted send fails — att_fail's attempt.failed must
			// deliver, or its errgroup sibling cancels the exhausted Exec
			// mid-flight instead of exercising the clear-on-failure path.
			sinkFailOn: map[string]bool{"att_fail/" + topicExhaust: true},
		},
	})

	// Paced one at a time: the fail-then-retry sequence on one window key
	// needs deterministic delivery order.
	dest, tenant, eventID := "dest_ef", "tenant_ef", "evt_ef"
	cmFail, msgFail := newCountingMessage(makeExhaustedEntry(dest, tenant, eventID, "att_fail", 4))
	cmOK, msgOK := newCountingMessage(makeExhaustedEntry(dest, tenant, eventID, "att_ok", 5))
	h.add(msgFail)
	h.waitTerminal([]*countingMessage{cmFail})
	h.add(msgOK)
	h.waitTerminal([]*countingMessage{cmOK})

	// The failed emit nacks; the retry (key cleared on failure) delivers and acks.
	cmFail.requireNacked(t)
	cmOK.requireAcked(t)
	assert.ElementsMatch(t, []string{topicFailed, topicFailed, topicExhaust}, topics(h.sink.forDest(dest)),
		"retry after emit failure re-delivers because the window key was cleared")
}

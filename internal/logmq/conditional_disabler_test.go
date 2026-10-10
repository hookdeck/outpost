package logmq_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// conditionalDisabler implements logmq.ConditionalDestinationDisabler like
// the tenant store's DisableDestination: only the first call for a
// destination changes it.
type conditionalDisabler struct {
	mu       sync.Mutex
	disabled map[string]bool
	calls    int
	plain    int
	err      error
}

func (d *conditionalDisabler) DisableDestination(ctx context.Context, tenantID, destinationID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.plain++
	return nil
}

func (d *conditionalDisabler) DisableDestinationIfEnabled(ctx context.Context, tenantID, destinationID string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.err != nil {
		return false, d.err
	}
	if d.disabled == nil {
		d.disabled = map[string]bool{}
	}
	key := tenantID + "/" + destinationID
	if d.disabled[key] {
		return false, nil
	}
	d.disabled[key] = true
	return true, nil
}

var _ logmq.ConditionalDestinationDisabler = (*conditionalDisabler)(nil)

// Only the call that actually disables the destination emits
// alert.destination.disabled; later failures past the threshold (in-flight
// attempts finishing after the disable) still emit their
// consecutive-failure alert.
func TestConditionalDisabler_EmitsDisabledOnlyWhenChanged(t *testing.T) {
	t.Parallel()
	disabler := &conditionalDisabler{}
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{disabler: disabler, autoDisableCount: 2, thresholds: []int{100}},
	})

	destID, tenant := "dest_cond", "tenant_cond"
	for i := 1; i <= 4; i++ {
		cm, msg := newCountingMessage(makeEntry(destID, tenant, fmt.Sprintf("att_%d", i), models.AttemptStatusFailed))
		h.add(msg)
		h.waitTerminal([]*countingMessage{cm})
		cm.requireAcked(t)
	}

	recs := h.sink.forDest(destID)
	assert.Equal(t, []string{"att_2"}, attemptIDs(forTopic(recs, topicDisabled)), "one disabled event, from the attempt that disabled it")
	assert.Equal(t, []string{"att_2", "att_3", "att_4"}, attemptIDs(forTopic(recs, topicCF)))
	disabler.mu.Lock()
	defer disabler.mu.Unlock()
	assert.Equal(t, 3, disabler.calls)
	assert.Zero(t, disabler.plain, "the conditional method replaces DisableDestination")
}

// A failing conditional disable nacks the entry, like a failing
// DisableDestination.
func TestConditionalDisabler_ErrorNacks(t *testing.T) {
	t.Parallel()
	disabler := &conditionalDisabler{err: fmt.Errorf("store down")}
	h := newHarness(t, harnessConfig{
		batcher: batcherConfig{itemCount: 1},
		alert:   alertConfig{disabler: disabler, autoDisableCount: 1, thresholds: []int{100}},
	})

	cm, msg := newCountingMessage(makeEntry("dest_err", "tenant_err", "att_1", models.AttemptStatusFailed))
	h.add(msg)
	h.waitTerminal([]*countingMessage{cm})
	require.EqualValues(t, 1, cm.nacks())
	assert.Empty(t, forTopic(h.sink.forDest("dest_err"), topicDisabled))
}

package logmq

import (
	"testing"

	"github.com/hookdeck/outpost/internal/alert"
	"github.com/stretchr/testify/assert"
)

// alert.ResetDestination ends the exhausted-retries suppression window of a
// destination, so both must name the same key.
func TestExhaustedRetriesKeyMatchesAlert(t *testing.T) {
	assert.Equal(t, exhaustedRetriesKey("tenant_1", "dest_1"), alert.ExhaustedRetriesKey("tenant_1", "dest_1"))
}

package logmq

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExhaustedRetriesKey_IDsWithColons(t *testing.T) {
	t.Parallel()

	assert.NotEqual(t,
		exhaustedRetriesKey("org", "123:webhook"),
		exhaustedRetriesKey("org:123", "webhook"))
	assert.Equal(t, "opevents:exhausted:tenant_1:des_1", exhaustedRetriesKey("tenant_1", "des_1"),
		"IDs without special characters keep their key")
}

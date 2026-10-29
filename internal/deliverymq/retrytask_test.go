package deliverymq

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryTask_OldPayloadFallsBackToUnboundedLookup(t *testing.T) {
	t.Parallel()
	var task RetryTask
	require.NoError(t, task.FromString(`{"EventID":"e","TenantID":"t","DestinationID":"d","Telemetry":null}`))

	assert.True(t, task.AttemptTime.IsZero())
	req := task.attemptLookupRequest()
	assert.Nil(t, req.TimeFilter.GTE)
	assert.Equal(t, []string{"e"}, req.EventIDs)
}

func TestRetryTask_RoundTripKeepsAttemptTime(t *testing.T) {
	t.Parallel()
	attemptTime := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)
	in := RetryTask{EventID: "e", TenantID: "t", DestinationID: "d", AttemptTime: attemptTime}

	str, err := in.ToString()
	require.NoError(t, err)
	var out RetryTask
	require.NoError(t, out.FromString(str))

	assert.True(t, attemptTime.Equal(out.AttemptTime))
}

func TestRetryTask_ZeroAttemptTimeOmitted(t *testing.T) {
	t.Parallel()
	str, err := (&RetryTask{EventID: "e", TenantID: "t", DestinationID: "d"}).ToString()
	require.NoError(t, err)
	assert.NotContains(t, str, "AttemptTime")
}

func TestRetryTask_LookupBoundedByAttemptTime(t *testing.T) {
	t.Parallel()
	task := RetryTask{EventID: "e", TenantID: "t", DestinationID: "d", AttemptTime: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}

	req := task.attemptLookupRequest()
	require.NotNil(t, req.TimeFilter.GTE)
	assert.True(t, req.TimeFilter.GTE.Equal(time.Date(2026, 1, 2, 3, 4, 4, 0, time.UTC)))
}

func TestNotReadyRequeueDelay(t *testing.T) {
	t.Parallel()
	flush := 10 * time.Second
	assert.Equal(t, flush, notReadyRequeueDelay(2*time.Second, flush))
	assert.Equal(t, time.Minute, notReadyRequeueDelay(2*time.Minute, flush))
	assert.Equal(t, maxNotReadyDelay, notReadyRequeueDelay(14*time.Minute, flush))
	assert.Equal(t, time.Second, notReadyRequeueDelay(0, 0))
}

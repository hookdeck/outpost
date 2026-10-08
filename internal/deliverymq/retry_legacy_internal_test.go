package deliverymq

import (
	"errors"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/rsmq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePollClient returns queued poll results in order and counts polls.
type fakePollClient struct {
	rsmq.Client
	results   []rsmq.PollResult
	errs      []error
	polls     int
	deleteErr error
}

func (f *fakePollClient) ReceiveMessagePoll(string, uint) (rsmq.PollResult, error) {
	i := f.polls
	f.polls++
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	if i < len(f.results) {
		return f.results[i], err
	}
	return rsmq.PollResult{}, err
}

func (f *fakePollClient) DeleteMessage(string, string) error {
	return f.deleteErr
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newTestDrainClient(current, legacy *fakePollClient) (*legacyDrainClient, *fakeClock) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	c := newLegacyDrainClient(current, legacy, nil)
	c.now = clock.now
	return c, clock
}

func TestLegacyDrainClient_SkipsEmptyLegacyQueueUntilRecheck(t *testing.T) {
	legacyMsg := &rsmq.QueueMessage{ID: "legacy-1"}
	legacy := &fakePollClient{results: []rsmq.PollResult{
		{},                   // empty: start skipping
		{Message: legacyMsg}, // written by an older instance meanwhile
	}}
	current := &fakePollClient{}
	c, clock := newTestDrainClient(current, legacy)

	_, err := c.ReceiveMessagePoll("q", 0)
	require.NoError(t, err)
	assert.Equal(t, 1, legacy.polls)
	assert.Equal(t, 1, current.polls)

	clock.t = clock.t.Add(legacyRecheckInterval - time.Second)
	_, err = c.ReceiveMessagePoll("q", 0)
	require.NoError(t, err)
	assert.Equal(t, 1, legacy.polls, "empty legacy queue should not be polled before the recheck deadline")
	assert.Equal(t, 2, current.polls)

	clock.t = clock.t.Add(time.Second)
	res, err := c.ReceiveMessagePoll("q", 0)
	require.NoError(t, err)
	assert.Equal(t, 2, legacy.polls)
	assert.Same(t, legacyMsg, res.Message, "legacy message should be picked up after the deadline")
}

func TestLegacyDrainClient_KeepsPollingLegacyQueueWithPendingMessages(t *testing.T) {
	legacy := &fakePollClient{results: []rsmq.PollResult{
		{HasNext: true, NextDue: 5 * time.Second},
		{HasNext: true, NextDue: 4 * time.Second},
	}}
	current := &fakePollClient{results: []rsmq.PollResult{
		{HasNext: true, NextDue: 10 * time.Second},
		{},
	}}
	c, _ := newTestDrainClient(current, legacy)

	res, err := c.ReceiveMessagePoll("q", 0)
	require.NoError(t, err)
	assert.True(t, res.HasNext)
	assert.Equal(t, 5*time.Second, res.NextDue, "NextDue should be the earliest of both queues")

	res, err = c.ReceiveMessagePoll("q", 0)
	require.NoError(t, err)
	assert.Equal(t, 2, legacy.polls, "legacy queue with pending messages should be polled every time")
	assert.True(t, res.HasNext)
	assert.Equal(t, 4*time.Second, res.NextDue)
}

func TestLegacyDrainClient_LegacyErrorDoesNotStopCurrentQueue(t *testing.T) {
	msg := &rsmq.QueueMessage{ID: "current-1"}
	legacy := &fakePollClient{errs: []error{errors.New("WRONGTYPE")}}
	current := &fakePollClient{results: []rsmq.PollResult{{Message: msg}, {}}}
	c, clock := newTestDrainClient(current, legacy)

	res, err := c.ReceiveMessagePoll("q", 0)
	require.NoError(t, err, "a legacy-only error must not surface as a receive error")
	assert.Same(t, msg, res.Message)

	_, err = c.ReceiveMessagePoll("q", 0)
	require.NoError(t, err)
	assert.Equal(t, 1, legacy.polls, "failing legacy queue should wait for the recheck deadline")

	clock.t = clock.t.Add(legacyRecheckInterval)
	_, err = c.ReceiveMessagePoll("q", 0)
	require.NoError(t, err)
	assert.Equal(t, 2, legacy.polls)
}

func TestLegacyDrainClient_DeleteMessage(t *testing.T) {
	redisErr := errors.New("WRONGTYPE")
	tests := []struct {
		name      string
		current   error
		legacy    error
		wantErrIs error
	}{
		{name: "deleted from current", current: nil, legacy: rsmq.ErrMessageNotFound},
		{name: "deleted from legacy", current: rsmq.ErrMessageNotFound, legacy: nil},
		{name: "in neither", current: rsmq.ErrMessageNotFound, legacy: rsmq.ErrQueueNotFound, wantErrIs: rsmq.ErrMessageNotFound},
		{name: "legacy error after current delete is ignored", current: nil, legacy: redisErr},
		{name: "legacy error when not in current", current: rsmq.ErrMessageNotFound, legacy: redisErr, wantErrIs: redisErr},
		{name: "current error", current: redisErr, legacy: nil, wantErrIs: redisErr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestDrainClient(&fakePollClient{deleteErr: tt.current}, &fakePollClient{deleteErr: tt.legacy})
			err := c.DeleteMessage("q", "id")
			if tt.wantErrIs != nil {
				assert.ErrorIs(t, err, tt.wantErrIs)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

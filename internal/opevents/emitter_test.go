package opevents_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSink records events sent to it and can be configured to fail.
type mockSink struct {
	mu     sync.Mutex
	events []*opevents.OperationEvent
	errs   []error // errors to return in order; nil entries mean success
	calls  int
}

func (s *mockSink) Init(ctx context.Context) error { return nil }
func (s *mockSink) Close() error                   { return nil }

func (s *mockSink) Send(ctx context.Context, event *opevents.OperationEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := s.calls
	s.calls++

	if idx < len(s.errs) && s.errs[idx] != nil {
		return s.errs[idx]
	}
	s.events = append(s.events, event)
	return nil
}

func (s *mockSink) sentEvents() []*opevents.OperationEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*opevents.OperationEvent(nil), s.events...)
}

func TestEmitter_Emit(t *testing.T) {
	t.Parallel()

	t.Run("wildcard topic filter accepts all topics", func(t *testing.T) {
		t.Parallel()
		sink := &mockSink{}
		em := opevents.NewEmitter(sink, "deploy-1", []string{"*"})

		err := em.Emit(context.Background(), "any.topic", "tenant-1", map[string]string{"key": "val"})
		require.NoError(t, err)

		events := sink.sentEvents()
		require.Len(t, events, 1)
		assert.Equal(t, "any.topic", events[0].Topic)
		assert.Equal(t, "tenant-1", events[0].TenantID)
		assert.Equal(t, "deploy-1", events[0].DeploymentID)
	})

	t.Run("specific topic filter passes matching topics", func(t *testing.T) {
		t.Parallel()
		sink := &mockSink{}
		em := opevents.NewEmitter(sink, "", []string{
			opevents.TopicAlertConsecutiveFailure,
			opevents.TopicTenantSubscriptionUpdated,
		})

		err := em.Emit(context.Background(), opevents.TopicAlertConsecutiveFailure, "t1", "data")
		require.NoError(t, err)
		assert.Len(t, sink.sentEvents(), 1)
	})

	t.Run("specific topic filter drops non-matching topics", func(t *testing.T) {
		t.Parallel()
		sink := &mockSink{}
		em := opevents.NewEmitter(sink, "", []string{opevents.TopicAlertConsecutiveFailure})

		err := em.Emit(context.Background(), opevents.TopicTenantSubscriptionUpdated, "t1", "data")
		require.NoError(t, err)
		assert.Empty(t, sink.sentEvents())
	})

	t.Run("envelope fields are populated", func(t *testing.T) {
		t.Parallel()
		sink := &mockSink{}
		em := opevents.NewEmitter(sink, "prod", []string{"*"})

		type payload struct {
			Count int `json:"count"`
		}

		err := em.Emit(context.Background(), "test.topic", "tenant-42", payload{Count: 7})
		require.NoError(t, err)

		events := sink.sentEvents()
		require.Len(t, events, 1)
		evt := events[0]

		assert.NotEmpty(t, evt.ID, "ID should be generated")
		assert.False(t, evt.Time.IsZero(), "Time should be set")
		assert.Equal(t, "test.topic", evt.Topic)
		assert.Equal(t, "prod", evt.DeploymentID)
		assert.Equal(t, "tenant-42", evt.TenantID)

		var got payload
		require.NoError(t, json.Unmarshal(evt.Data, &got))
		assert.Equal(t, 7, got.Count)
	})

	t.Run("deployment_id omitted when empty", func(t *testing.T) {
		t.Parallel()
		sink := &mockSink{}
		em := opevents.NewEmitter(sink, "", []string{"*"})

		err := em.Emit(context.Background(), "t", "tenant", "data")
		require.NoError(t, err)

		events := sink.sentEvents()
		require.Len(t, events, 1)
		assert.Empty(t, events[0].DeploymentID)

		// Verify omitempty works in JSON
		b, err := json.Marshal(events[0])
		require.NoError(t, err)
		assert.NotContains(t, string(b), "deployment_id")
	})

	t.Run("empty topics returns noop emitter", func(t *testing.T) {
		t.Parallel()
		sink := &mockSink{}
		em := opevents.NewEmitter(sink, "", []string{})

		err := em.Emit(context.Background(), "any.topic", "t1", "data")
		require.NoError(t, err)
		assert.Empty(t, sink.sentEvents(), "noop emitter should not send events")
	})

	t.Run("retry succeeds after transient errors", func(t *testing.T) {
		t.Parallel()
		sink := &mockSink{
			errs: []error{
				errors.New("fail-1"),
				errors.New("fail-2"),
				nil, // third attempt succeeds
			},
		}
		em := opevents.NewEmitter(sink, "", []string{"*"})

		err := em.Emit(context.Background(), "t", "t1", "data")
		require.NoError(t, err)
		assert.Len(t, sink.sentEvents(), 1)
	})

	t.Run("retry exhausted returns error", func(t *testing.T) {
		t.Parallel()
		sink := &mockSink{
			errs: []error{
				errors.New("fail-1"),
				errors.New("fail-2"),
				errors.New("fail-3"),
			},
		}
		em := opevents.NewEmitter(sink, "", []string{"*"})

		err := em.Emit(context.Background(), "t", "t1", "data")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fail-3")
		assert.Empty(t, sink.sentEvents())
	})

	t.Run("context cancellation stops retry", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		sink := &mockSink{
			errs: []error{errors.New("fail")},
		}
		em := opevents.NewEmitter(sink, "", []string{"*"})

		err := em.Emit(ctx, "t", "t1", "data")
		require.Error(t, err)
	})
}

package publishmq_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockQueueMessage struct {
	acked    bool
	nacked   bool
	rejected bool
}

func (m *mockQueueMessage) Ack()    { m.acked = true }
func (m *mockQueueMessage) Nack()   { m.nacked = true }
func (m *mockQueueMessage) Reject() { m.rejected = true }

type mockEventHandler struct {
	calls  []*models.Event
	result *publishmq.HandleResult
	err    error
}

func (m *mockEventHandler) Handle(_ context.Context, event *models.Event) (*publishmq.HandleResult, error) {
	m.calls = append(m.calls, event)
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	return &publishmq.HandleResult{EventID: event.ID}, nil
}

func TestMessageHandler_InvalidData(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"null data", `{"tenant_id":"t1","data":null}`},
		{"string data", `{"tenant_id":"t1","data":"hello"}`},
		{"number data", `{"tenant_id":"t1","data":42}`},
		{"array data", `{"tenant_id":"t1","data":[1,2,3]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eh := &mockEventHandler{}
			handler := publishmq.NewMessageHandler(eh)

			qm := &mockQueueMessage{}
			msg := &mqs.Message{
				QueueMessage: qm,
				Body:         []byte(tt.body),
			}

			err := handler.Handle(context.Background(), msg)

			require.ErrorIs(t, err, publishmq.ErrInvalidData)
			assert.True(t, qm.rejected, "message should be rejected")
			assert.False(t, qm.nacked, "message should not be nacked")
			assert.Empty(t, eh.calls, "event handler should not be called")
		})
	}
}

func TestMessageHandler_InvalidMetadata(t *testing.T) {
	eh := &mockEventHandler{}
	handler := publishmq.NewMessageHandler(eh)

	qm := &mockQueueMessage{}
	msg := &mqs.Message{
		QueueMessage: qm,
		Body:         []byte(`{"tenant_id":"t1","metadata":{"count":42},"data":{"key":"value"}}`),
	}

	err := handler.Handle(context.Background(), msg)

	// json.Unmarshal fails because metadata is map[string]string and 42 is not a string
	require.Error(t, err)
	assert.True(t, qm.rejected, "message should be rejected")
	assert.Empty(t, eh.calls, "event handler should not be called")
}

func TestMessageHandler_InvalidJSON(t *testing.T) {
	eh := &mockEventHandler{}
	handler := publishmq.NewMessageHandler(eh)

	qm := &mockQueueMessage{}
	msg := &mqs.Message{
		QueueMessage: qm,
		LoggableID:   "msg_1",
		Body:         []byte(`not json`),
	}

	err := handler.Handle(context.Background(), msg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "msg_1", "error should identify the rejected message")
	assert.True(t, qm.rejected, "message should be rejected")
	assert.Empty(t, eh.calls, "event handler should not be called")
}

func TestMessageHandler_EventHandlerError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantRejected bool
	}{
		{"invalid topic", publishmq.ErrInvalidTopic, false},
		{"required topic", publishmq.ErrRequiredTopic, true},
		{"transient error", errors.New("redis: connection refused"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eh := &mockEventHandler{err: tt.err}
			handler := publishmq.NewMessageHandler(eh)

			qm := &mockQueueMessage{}
			msg := &mqs.Message{
				QueueMessage: qm,
				Body:         []byte(`{"tenant_id":"t1","topic":"user.created","data":{"key":"value"}}`),
			}

			err := handler.Handle(context.Background(), msg)

			require.ErrorIs(t, err, tt.err)
			assert.Equal(t, tt.wantRejected, qm.rejected, "rejected")
			assert.Equal(t, !tt.wantRejected, qm.nacked, "nacked")
			assert.False(t, qm.acked, "message should not be acked")
		})
	}
}

func TestMessageHandler_RejectFallsBackToNack(t *testing.T) {
	handler := publishmq.NewMessageHandler(&mockEventHandler{})

	qm := &nackOnlyQueueMessage{}
	msg := &mqs.Message{
		QueueMessage: qm,
		Body:         []byte(`not json`),
	}

	require.Error(t, handler.Handle(context.Background(), msg))
	assert.True(t, qm.nacked, "message should be nacked when the broker cannot reject")
}

func TestMessageHandler_ZeroRedeliveries(t *testing.T) {
	counter := &fakeRedeliveryCounter{err: errors.New("must not be called")}
	handler := publishmq.NewMessageHandler(
		&mockEventHandler{err: errors.New("transient")},
		publishmq.WithMaxRedeliveries(0, counter),
	)
	body := []byte(`{"tenant_id":"t1","topic":"user.created","data":{}}`)

	qm := &mockQueueMessage{}
	err := handler.Handle(context.Background(), &mqs.Message{QueueMessage: qm, ID: "msg_1", Body: body})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "must not be called")
	assert.True(t, qm.rejected, "first failure should be rejected")

	nq := &nackOnlyQueueMessage{}
	err = handler.Handle(context.Background(), &mqs.Message{QueueMessage: nq, ID: "msg_2", Body: body})
	require.Error(t, err)
	assert.True(t, nq.acked, "first failure should be acked where the broker cannot reject")
}

func TestMessageHandler_MaxRedeliveriesAcksNonRejectable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		body string
	}{
		{"transient error", errors.New("transient"), `{"tenant_id":"t1","topic":"user.created","data":{}}`},
		{"invalid message", nil, `not json`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := publishmq.NewMessageHandler(
				&mockEventHandler{err: tt.err},
				publishmq.WithMaxRedeliveries(1, &fakeRedeliveryCounter{}),
			)
			receive := func() (*nackOnlyQueueMessage, error) {
				qm := &nackOnlyQueueMessage{}
				err := handler.Handle(context.Background(), &mqs.Message{
					QueueMessage: qm,
					LoggableID:   "msg_1",
					ID:           "msg_1",
					Body:         []byte(tt.body),
				})
				return qm, err
			}

			qm, err := receive()
			require.Error(t, err)
			assert.True(t, qm.nacked, "first failure should be nacked")

			qm, err = receive()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "dropped message msg_1")
			assert.True(t, qm.acked, "message should be acked once redeliveries run out")
			assert.False(t, qm.nacked)
		})
	}
}

type nackOnlyQueueMessage struct {
	acked  bool
	nacked bool
}

func (m *nackOnlyQueueMessage) Ack()  { m.acked = true }
func (m *nackOnlyQueueMessage) Nack() { m.nacked = true }

type fakeRedeliveryCounter struct {
	counts map[string]int64
	err    error
}

func (c *fakeRedeliveryCounter) Incr(_ context.Context, key string) (int64, error) {
	if c.err != nil {
		return 0, c.err
	}
	if c.counts == nil {
		c.counts = map[string]int64{}
	}
	c.counts[key]++
	return c.counts[key], nil
}

func TestMessageHandler_MaxRedeliveries(t *testing.T) {
	transientErr := errors.New("redis: connection refused")
	counter := &fakeRedeliveryCounter{}
	handler := publishmq.NewMessageHandler(
		&mockEventHandler{err: transientErr},
		publishmq.WithMaxRedeliveries(2, counter),
	)

	receive := func() (*mockQueueMessage, error) {
		qm := &mockQueueMessage{}
		err := handler.Handle(context.Background(), &mqs.Message{
			QueueMessage: qm,
			LoggableID:   "msg_1",
			ID:           "msg_1",
			Body:         []byte(`{"tenant_id":"t1","topic":"user.created","data":{"key":"value"}}`),
		})
		return qm, err
	}

	// The first receive and the first redelivery are nacked, so the
	// message is redelivered twice.
	for i := 0; i < 2; i++ {
		qm, err := receive()
		require.ErrorIs(t, err, transientErr)
		assert.True(t, qm.nacked, "receive %d should be nacked", i+1)
		assert.False(t, qm.rejected, "receive %d should not be rejected", i+1)
	}

	qm, err := receive()
	require.ErrorIs(t, err, transientErr)
	assert.Contains(t, err.Error(), "msg_1", "error should identify the rejected message")
	assert.True(t, qm.rejected, "the second redelivery should be rejected")
	assert.False(t, qm.nacked, "message should not be nacked after max redeliveries")
}

func TestMessageHandler_MaxRedeliveriesKeyFallsBackToBody(t *testing.T) {
	counter := &fakeRedeliveryCounter{}
	handler := publishmq.NewMessageHandler(
		&mockEventHandler{err: errors.New("transient")},
		publishmq.WithMaxRedeliveries(1, counter),
	)

	for _, body := range []string{
		`{"tenant_id":"t1","topic":"a","data":{}}`,
		`{"tenant_id":"t1","topic":"b","data":{}}`,
	} {
		qm := &mockQueueMessage{}
		_ = handler.Handle(context.Background(), &mqs.Message{QueueMessage: qm, Body: []byte(body)})
		assert.True(t, qm.nacked, "distinct bodies are counted separately")
	}
	assert.Len(t, counter.counts, 2)
}

func TestMessageHandler_MaxRedeliveriesCounterError(t *testing.T) {
	handler := publishmq.NewMessageHandler(
		&mockEventHandler{err: errors.New("transient")},
		publishmq.WithMaxRedeliveries(1, &fakeRedeliveryCounter{err: errors.New("redis down")}),
	)

	for i := 0; i < 3; i++ {
		qm := &mockQueueMessage{}
		err := handler.Handle(context.Background(), &mqs.Message{
			QueueMessage: qm,
			ID:           "msg_1",
			Body:         []byte(`{"tenant_id":"t1","topic":"user.created","data":{}}`),
		})
		require.Error(t, err)
		assert.True(t, qm.nacked, "message should be redelivered when it cannot be counted")
	}
}

func TestMessageHandler_MaxRedeliveriesSkipsInvalidMessages(t *testing.T) {
	counter := &fakeRedeliveryCounter{}
	handler := publishmq.NewMessageHandler(&mockEventHandler{}, publishmq.WithMaxRedeliveries(5, counter))

	qm := &mockQueueMessage{}
	require.Error(t, handler.Handle(context.Background(), &mqs.Message{QueueMessage: qm, ID: "msg_1", Body: []byte(`not json`)}))
	assert.True(t, qm.rejected, "invalid message should be rejected on the first failure")
	assert.Empty(t, counter.counts, "invalid messages are not counted")
}

func TestMessageHandler_SuccessSkipsCounter(t *testing.T) {
	counter := &fakeRedeliveryCounter{err: errors.New("must not be called")}
	handler := publishmq.NewMessageHandler(&mockEventHandler{}, publishmq.WithMaxRedeliveries(1, counter))

	qm := &mockQueueMessage{}
	require.NoError(t, handler.Handle(context.Background(), &mqs.Message{
		QueueMessage: qm,
		ID:           "msg_1",
		Body:         []byte(`{"tenant_id":"t1","topic":"user.created","data":{}}`),
	}))
	assert.True(t, qm.acked)
}

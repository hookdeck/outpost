package publishmq_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockQueueMessage struct {
	acked  bool
	nacked bool
}

func (m *mockQueueMessage) Ack()  { m.acked = true }
func (m *mockQueueMessage) Nack() { m.nacked = true }

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
			assert.True(t, qm.nacked, "message should be nacked")
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
	assert.True(t, qm.nacked, "message should be nacked")
	assert.Empty(t, eh.calls, "event handler should not be called")
}

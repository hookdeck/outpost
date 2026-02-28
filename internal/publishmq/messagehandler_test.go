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

func TestMessageHandler_NullData(t *testing.T) {
	eh := &mockEventHandler{}
	handler := publishmq.NewMessageHandler(eh)

	qm := &mockQueueMessage{}
	msg := &mqs.Message{
		QueueMessage: qm,
		Body:         []byte(`{"tenant_id":"t1","data":null}`),
	}

	err := handler.Handle(context.Background(), msg)

	require.ErrorIs(t, err, publishmq.ErrInvalidData)
	assert.True(t, qm.nacked, "message should be nacked")
	assert.Empty(t, eh.calls, "event handler should not be called")
}

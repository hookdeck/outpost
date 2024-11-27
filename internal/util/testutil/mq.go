package testutil

import "github.com/hookdeck/outpost/internal/mqs"

type MockMsg struct {
	ID string
}

var _ mqs.IncomingMessage = &MockMsg{}

func (m *MockMsg) FromMessage(msg *mqs.Message) error {
	m.ID = string(msg.Body)
	return nil
}

func (m *MockMsg) ToMessage() (*mqs.Message, error) {
	return &mqs.Message{Body: []byte(m.ID)}, nil
}

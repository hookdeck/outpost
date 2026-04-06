package opevents

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ReceivedEvent is a single operation event received by the mock server.
type ReceivedEvent struct {
	Event     OperationEvent
	Signature string // raw X-Outpost-Signature header
	Body      []byte // raw request body (for signature verification)
}

// OperationEvent mirrors internal/opevents.OperationEvent for test decoding.
type OperationEvent struct {
	ID           string          `json:"id"`
	Topic        string          `json:"topic"`
	Time         time.Time       `json:"time"`
	DeploymentID string          `json:"deployment_id,omitempty"`
	TenantID     string          `json:"tenant_id,omitempty"`
	Data         json.RawMessage `json:"data"`
}

// MockServer captures operation events sent to its HTTP endpoint.
type MockServer struct {
	server *http.Server
	events []ReceivedEvent
	mu     sync.RWMutex
	port   int
}

func NewMockServer() *MockServer {
	s := &MockServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/opevents", s.handleEvent)
	s.server = &http.Server{
		Addr:    ":0",
		Handler: mux,
	}
	return s
}

func (s *MockServer) Start() error {
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	s.port = addr.Port
	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Printf("opevents mock server error: %v\n", err)
		}
	}()
	return nil
}

func (s *MockServer) Stop() error {
	return s.server.Close()
}

func (s *MockServer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = nil
}

func (s *MockServer) GetURL() string {
	return fmt.Sprintf("http://localhost:%d/opevents", s.port)
}

func (s *MockServer) GetEvents() []ReceivedEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ReceivedEvent, len(s.events))
	copy(out, s.events)
	return out
}

// GetEventsByTopic returns events matching the given topic.
func (s *MockServer) GetEventsByTopic(topic string) []ReceivedEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var filtered []ReceivedEvent
	for _, e := range s.events {
		if e.Event.Topic == topic {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// VerifySignature checks that the event's HMAC signature matches the given secret.
func VerifySignature(event ReceivedEvent, secret string) bool {
	if secret == "" || event.Signature == "" {
		return false
	}
	sig := strings.TrimPrefix(event.Signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(event.Body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

func (s *MockServer) handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var event OperationEvent
	if err := json.Unmarshal(body, &event); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.events = append(s.events, ReceivedEvent{
		Event:     event,
		Signature: r.Header.Get("X-Outpost-Signature"),
		Body:      body,
	})
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

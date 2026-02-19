package alert

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

type AlertRequest struct {
	Alert      AlertPayload
	AuthHeader string
}

type AlertPayload struct {
	Topic     string          `json:"topic"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// ConsecutiveFailureAlert is a parsed alert for "alert.consecutive_failure"
type ConsecutiveFailureAlert struct {
	Topic     string                 `json:"topic"`
	Timestamp time.Time              `json:"timestamp"`
	Data      ConsecutiveFailureData `json:"data"`
}

// DestinationDisabledAlert is a parsed alert for "alert.destination.disabled"
type DestinationDisabledAlert struct {
	Topic     string                  `json:"topic"`
	Timestamp time.Time               `json:"timestamp"`
	Data      DestinationDisabledData `json:"data"`
}

// AlertDestination matches internal/alert.AlertDestination
type AlertDestination struct {
	ID         string            `json:"id"`
	TenantID   string            `json:"tenant_id"`
	Type       string            `json:"type"`
	Topics     []string          `json:"topics"`
	Filter     map[string]any    `json:"filter,omitempty"`
	Config     map[string]string `json:"config"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	DisabledAt *time.Time        `json:"disabled_at"`
}

// ConsecutiveFailures represents the nested consecutive failure state
type ConsecutiveFailures struct {
	Current   int `json:"current"`
	Max       int `json:"max"`
	Threshold int `json:"threshold"`
}

// ConsecutiveFailureData matches internal/alert.ConsecutiveFailureData
type ConsecutiveFailureData struct {
	TenantID            string              `json:"tenant_id"`
	Attempt             *models.Attempt     `json:"attempt"`
	Event               *models.Event       `json:"event"`
	Destination         *AlertDestination   `json:"destination"`
	ConsecutiveFailures ConsecutiveFailures `json:"consecutive_failures"`
}

// DestinationDisabledData matches the expected payload for "alert.destination.disabled"
type DestinationDisabledData struct {
	TenantID    string            `json:"tenant_id"`
	Destination *AlertDestination `json:"destination"`
	DisabledAt  time.Time         `json:"disabled_at"`
	Reason      string            `json:"reason"`
	Attempt     *models.Attempt   `json:"attempt,omitempty"`
	Event       *models.Event     `json:"event,omitempty"`
}

type AlertMockServer struct {
	server *http.Server
	alerts []AlertRequest
	mu     sync.RWMutex
	port   int
}

func NewAlertMockServer() *AlertMockServer {
	s := &AlertMockServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/alert", s.handleAlert)

	s.server = &http.Server{
		Addr:    ":0", // Random port
		Handler: mux,
	}

	return s
}

func (s *AlertMockServer) Start() error {
	// Create listener on random port
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	// Get the actual port
	addr := listener.Addr().(*net.TCPAddr)
	s.port = addr.Port

	// Start server in background
	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("alert mock server error: %v", err)
		}
	}()

	return nil
}

func (s *AlertMockServer) Stop() error {
	return s.server.Close()
}

func (s *AlertMockServer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alerts = nil
}

func (s *AlertMockServer) GetAlerts() []AlertRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	alerts := make([]AlertRequest, len(s.alerts))
	copy(alerts, s.alerts)
	return alerts
}

func (s *AlertMockServer) GetCallbackURL() string {
	return fmt.Sprintf("http://localhost:%d/alert", s.port)
}

func (s *AlertMockServer) handleAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var payload AlertPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	request := AlertRequest{
		Alert:      payload,
		AuthHeader: r.Header.Get("Authorization"),
	}

	s.mu.Lock()
	s.alerts = append(s.alerts, request)
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

// alertDataWithDestination is used to extract destination from any alert type
type alertDataWithDestination struct {
	Destination *AlertDestination `json:"destination"`
}

// Helper methods for assertions
func (s *AlertMockServer) GetAlertsForDestination(destinationID string) []AlertRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filtered []AlertRequest
	for _, alert := range s.alerts {
		var data alertDataWithDestination
		if err := json.Unmarshal(alert.Alert.Data, &data); err != nil {
			continue
		}
		if data.Destination != nil && data.Destination.ID == destinationID {
			filtered = append(filtered, alert)
		}
	}
	return filtered
}

func (s *AlertMockServer) GetLastAlert() *AlertRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.alerts) == 0 {
		return nil
	}
	alert := s.alerts[len(s.alerts)-1]
	return &alert
}

// GetAlertsForDestinationByTopic returns alerts filtered by destination ID and topic
func (s *AlertMockServer) GetAlertsForDestinationByTopic(destinationID, topic string) []AlertRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filtered []AlertRequest
	for _, alert := range s.alerts {
		if alert.Alert.Topic != topic {
			continue
		}
		var data alertDataWithDestination
		if err := json.Unmarshal(alert.Alert.Data, &data); err != nil {
			continue
		}
		if data.Destination != nil && data.Destination.ID == destinationID {
			filtered = append(filtered, alert)
		}
	}
	return filtered
}

// ParseConsecutiveFailureData parses the Data field as ConsecutiveFailureData
func (a *AlertRequest) ParseConsecutiveFailureData() (*ConsecutiveFailureData, error) {
	var data ConsecutiveFailureData
	if err := json.Unmarshal(a.Alert.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// ParseDestinationDisabledData parses the Data field as DestinationDisabledData
func (a *AlertRequest) ParseDestinationDisabledData() (*DestinationDisabledData, error) {
	var data DestinationDisabledData
	if err := json.Unmarshal(a.Alert.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

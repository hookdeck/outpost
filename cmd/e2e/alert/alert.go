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
	Topic     string                 `json:"topic"`
	Timestamp time.Time              `json:"timestamp"`
	Data      ConsecutiveFailureData `json:"data"`
}

type ConsecutiveFailureData struct {
	MaxConsecutiveFailures int                    `json:"max_consecutive_failures"`
	ConsecutiveFailures    int                    `json:"consecutive_failures"`
	WillDisable            bool                   `json:"will_disable"`
	Destination            *models.Destination    `json:"destination"`
	Data                   map[string]interface{} `json:"data"`
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

// Helper methods for assertions
func (s *AlertMockServer) GetAlertsForDestination(destinationID string) []AlertRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filtered []AlertRequest
	for _, alert := range s.alerts {
		if alert.Alert.Data.Destination != nil && alert.Alert.Data.Destination.ID == destinationID {
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

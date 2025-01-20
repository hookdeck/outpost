package alert

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/hookdeck/outpost/internal/models"
)

type AlertPayload struct {
	Topic               string                 `json:"topic"`
	DisableThreshold    int                    `json:"disable_threshold"`
	ConsecutiveFailures int                    `json:"consecutive_failures"`
	Destination         *models.Destination    `json:"destination"`
	Data                map[string]interface{} `json:"data"`
}

type AlertMockServer struct {
	server *http.Server
	alerts []AlertPayload
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

func (s *AlertMockServer) GetAlerts() []AlertPayload {
	s.mu.RLock()
	defer s.mu.RUnlock()
	alerts := make([]AlertPayload, len(s.alerts))
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

	s.mu.Lock()
	s.alerts = append(s.alerts, payload)
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

// Helper methods for assertions
func (s *AlertMockServer) GetAlertsForDestination(destinationID string) []AlertPayload {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var filtered []AlertPayload
	for _, alert := range s.alerts {
		if alert.Destination != nil && alert.Destination.ID == destinationID {
			filtered = append(filtered, alert)
		}
	}
	return filtered
}

func (s *AlertMockServer) GetLastAlert() *AlertPayload {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.alerts) == 0 {
		return nil
	}
	alert := s.alerts[len(s.alerts)-1]
	return &alert
}

package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/hookdeck/outpost/loadtest/mock/webhook/lru"
)

// Config holds server configuration options
type Config struct {
	EventTTL time.Duration
	MaxSize  int
}

// Server handles webhook events and provides APIs to check their delivery
type Server struct {
	events  *lru.Cache[string, *EventRecord]
	config  Config
	started time.Time
	stats   Stats
}

// EventRecord represents a stored webhook event
type EventRecord struct {
	ID         string                 `json:"id"`
	ReceivedAt time.Time              `json:"received_at"`
	Payload    map[string]interface{} `json:"payload"`
	Headers    map[string]string      `json:"headers"`
}

// Stats tracks server statistics
type Stats struct {
	EventsReceived int `json:"events_received"`
	EventsStored   int `json:"events_stored"`
}

// responseWriter is a wrapper for http.ResponseWriter that captures the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code before writing it
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// NewServer creates a new webhook server with the provided configuration
func NewServer(config Config) *Server {
	if config.EventTTL == 0 {
		config.EventTTL = 10 * time.Minute // Default TTL
	}
	if config.MaxSize == 0 {
		config.MaxSize = 10000 // Default max cache size
	}

	return &Server{
		events: lru.New[string, *EventRecord](
			config.MaxSize,
			config.EventTTL,
			nil, // No eviction callback needed
		),
		config:  config,
		started: time.Now(),
		stats:   Stats{},
	}
}

// Routes returns the HTTP routing configuration
func (s *Server) Routes() http.Handler {
	r := mux.NewRouter()

	// Core API routes
	r.HandleFunc("/webhook", s.handleWebhook).Methods("POST")
	r.HandleFunc("/events/{eventId}", s.getEvent).Methods("GET")
	r.HandleFunc("/health", s.healthCheck).Methods("GET")

	// Add middleware for request logging
	return logMiddleware(r)
}

// handleWebhook processes incoming webhook requests
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	// Extract the event ID from headers - try both supported header formats
	eventID := r.Header.Get("x-outpost-event-id")
	if eventID == "" {
		eventID = r.Header.Get("x-acme-event-id")
	}

	if eventID == "" {
		// Return 400 error if event ID is missing from both header formats
		http.Error(w, "Missing required header: x-outpost-event-id or x-acme-event-id", http.StatusBadRequest)
		log.Printf("ERROR: Request rejected - missing event ID header")
		return
	}

	// Read and parse the JSON body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		log.Printf("ERROR: Failed to read request body for event %s: %v", eventID, err)
		return
	}
	defer r.Body.Close()

	// Parse JSON body
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		log.Printf("ERROR: Failed to parse JSON for event %s: %v", eventID, err)
		return
	}

	// Collect headers
	headers := make(map[string]string)
	for name, values := range r.Header {
		if len(values) > 0 {
			headers[name] = values[0]
		}
	}

	// Create and store the event record
	event := &EventRecord{
		ID:         eventID,
		ReceivedAt: time.Now(),
		Payload:    payload,
		Headers:    headers,
	}

	s.events.Add(eventID, event)

	// Update stats
	s.stats.EventsReceived++
	s.stats.EventsStored = s.events.Len()

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"received": true,
		"id":       eventID,
	})

	// Log the successful webhook event
	payloadSize := len(body)
	log.Printf("Webhook received: id=%s size=%d bytes", eventID, payloadSize)
}

// getEvent retrieves a specific event by ID
func (s *Server) getEvent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	eventID := vars["eventId"]

	event, found := s.events.Get(eventID)
	if !found {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(event)
}

// healthCheck provides service health status
func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Calculate uptime
	uptime := time.Since(s.started)

	response := map[string]interface{}{
		"status":                "healthy",
		"events_received":       s.stats.EventsReceived,
		"events_stored":         s.stats.EventsStored,
		"events_stored_current": s.events.Len(),
		"uptime_seconds":        int(uptime.Seconds()),
	}

	json.NewEncoder(w).Encode(response)
}

// logMiddleware logs incoming requests with improved details
func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap the response writer to capture the status code
		wrappedWriter := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // Default status
		}

		// Process the request
		next.ServeHTTP(wrappedWriter, r)

		// Calculate duration
		duration := time.Since(start)

		// Log request details with status code
		log.Printf("%s %s - %d %s - %s",
			r.Method,
			r.RequestURI,
			wrappedWriter.statusCode,
			http.StatusText(wrappedWriter.statusCode),
			duration,
		)
	})
}

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/hookdeck/outpost/loadtest-app/internal/config"
	"github.com/hookdeck/outpost/loadtest-app/internal/mock"
)

type Server struct {
	cfg        *config.Config
	mux        *http.ServeMux
	server     *http.Server
	MockServer *mock.Server
}

func New(cfg *config.Config) *Server {
	mockServer := mock.NewServer(func(d mock.DeliveryRecord) {
		slog.Debug("delivery received", "event_id", d.EventID, "group", d.GroupName, "tenant", d.TenantIndex, "dest", d.DestIndex)
	})

	s := &Server{
		cfg:        cfg,
		mux:        http.NewServeMux(),
		MockServer: mockServer,
	}
	s.registerRoutes()
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: s.mux,
	}
	return s
}

func (s *Server) registerRoutes() {
	// API routes
	s.mux.HandleFunc("GET /api/status", s.handleStatus)

	// Webhook routes
	s.mux.HandleFunc("GET /webhook/health", s.handleWebhookHealth)
	s.mux.HandleFunc("POST /webhook/{group}/{tenant}/{dest}", s.MockServer.Handler().ServeHTTP)

	// Dashboard
	s.mux.HandleFunc("GET /", s.handleDashboard)
}

func (s *Server) SetDeliveryCallback(cb mock.DeliveryCallback) {
	s.MockServer = mock.NewServer(cb)
	// Re-register the webhook handler with updated callback
	s.mux.HandleFunc("POST /webhook/{group}/{tenant}/{dest}", s.MockServer.Handler().ServeHTTP)
}

func (s *Server) Start() error {
	slog.Info("starting server", "port", s.cfg.Port)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("shutting down server")
	return s.server.Shutdown(ctx)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"outpost_url": s.cfg.OutpostURL,
		"mock_url":    s.cfg.MockURL,
		"time":        time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleWebhookHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Outpost Load Test</title></head>
<body>
<h1>Outpost Load Test</h1>
<p>Outpost: %s</p>
<p>Mock URL: %s</p>
<p>Status: running</p>
</body>
</html>`, s.cfg.OutpostURL, s.cfg.MockURL)
}

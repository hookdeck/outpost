package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/hookdeck/outpost/loadtest-app/internal/config"
)

type Server struct {
	cfg    *config.Config
	mux    *http.ServeMux
	server *http.Server
}

func New(cfg *config.Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
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

	// Dashboard
	s.mux.HandleFunc("GET /", s.handleDashboard)
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

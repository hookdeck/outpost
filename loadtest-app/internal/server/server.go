package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/hookdeck/outpost/loadtest-app/internal/api"
	"github.com/hookdeck/outpost/loadtest-app/internal/config"
)

type Server struct {
	cfg    *config.Config
	mux    *http.ServeMux
	server *http.Server
	App    *api.App
}

func New(cfg *config.Config) *Server {
	app := api.NewApp(cfg)
	mux := http.NewServeMux()

	// Register API routes
	app.RegisterRoutes(mux)

	// Webhook routes
	mux.HandleFunc("GET /webhook/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("POST /webhook/{group}/{tenant}/{dest}", app.MockServer.Handler().ServeHTTP)

	// Dashboard placeholder
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
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
</html>`, cfg.OutpostURL, cfg.MockURL)
	})

	s := &Server{
		cfg: cfg,
		mux: mux,
		App: app,
		server: &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.Port),
			Handler: mux,
		},
	}
	return s
}

func (s *Server) Start() error {
	slog.Info("starting server", "port", s.cfg.Port)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("shutting down server")
	s.App.Stop()
	return s.server.Shutdown(ctx)
}

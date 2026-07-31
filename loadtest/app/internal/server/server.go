package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"

	"github.com/hookdeck/outpost/loadtest/app/internal/api"
	"github.com/hookdeck/outpost/loadtest/app/internal/config"
	"github.com/hookdeck/outpost/loadtest/app/internal/dashboard"
	"github.com/hookdeck/outpost/loadtest/app/internal/mockhttp"
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

	// Mock HTTP (httpbin-style)
	mockhttp.Register(mux)

	// Profiling. Registered before the dashboard's catch-all; ServeMux prefers
	// the longer pattern, so these win for /debug/pprof/* and nothing else
	// changes. The RateCounter leak took a code read and a simulation to find
	// because no heap profile was reachable on a running deployment.
	if cfg.PprofEnabled {
		mux.HandleFunc("GET /debug/pprof/", pprof.Index)
		mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
		slog.Warn("pprof enabled on a public listener", "path", "/debug/pprof/")
	}

	// Dashboard
	mux.Handle("GET /", dashboard.Handler())

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

package services_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBaseRouter_Pprof(t *testing.T) {
	logger, err := logging.NewLogger(logging.WithLogLevel("error"))
	require.NoError(t, err)
	supervisor := worker.NewWorkerSupervisor(logger)

	get := func(r http.Handler, path string) int {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code
	}

	t.Run("disabled by default", func(t *testing.T) {
		r := services.NewBaseRouter(supervisor, "test", false)
		assert.Equal(t, http.StatusNotFound, get(r, "/debug/pprof/"))
		assert.Equal(t, http.StatusNotFound, get(r, "/debug/pprof/heap"))
		assert.Equal(t, http.StatusOK, get(r, "/healthz"))
	})

	t.Run("enabled", func(t *testing.T) {
		r := services.NewBaseRouter(supervisor, "test", true)
		assert.Equal(t, http.StatusOK, get(r, "/debug/pprof/"))
		assert.Equal(t, http.StatusOK, get(r, "/debug/pprof/heap"))
		assert.Equal(t, http.StatusOK, get(r, "/debug/pprof/goroutine"))
		assert.Equal(t, http.StatusOK, get(r, "/healthz"))
	})
}

func TestNewBaseRouter_HealthPaths(t *testing.T) {
	logger, err := logging.NewLogger(logging.WithLogLevel("error"))
	require.NoError(t, err)
	supervisor := worker.NewWorkerSupervisor(logger)
	supervisor.GetHealthTracker().MarkHealthy("http-server")
	r := services.NewBaseRouter(supervisor, "test", false)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	for _, path := range []string{"/healthz", "/api/v1/healthz", "/api/v2/healthz"} {
		t.Run(path, func(t *testing.T) {
			rec := get(path)
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
			var body struct {
				Status  string         `json:"status"`
				Workers map[string]any `json:"workers"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, "healthy", body.Status)
			assert.Contains(t, body.Workers, "http-server")
		})
	}

	// Other paths are left to the API router, which the services mount as the
	// base router's NoRoute handler.
	assert.Equal(t, http.StatusNotFound, get("/api/v3/healthz").Code)
}

func TestHealthHandler_StatusCode(t *testing.T) {
	logger, err := logging.NewLogger(logging.WithLogLevel("error"))
	require.NoError(t, err)
	supervisor := worker.NewWorkerSupervisor(logger)
	tracker := supervisor.GetHealthTracker()
	r := services.NewBaseRouter(supervisor, "test", false)

	get := func() (int, map[string]any) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		return rec.Code, body
	}

	tracker.MarkHealthy("http-server")
	tracker.MarkDegraded("publishmq-consumer", time.Now(), worker.ReasonRecoveryFailed)
	code, body := get()
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "degraded", body["status"])

	tracker.MarkFailedWithReason("publishmq-consumer", time.Now(), worker.ReasonRecoveryFailed)
	code, body = get()
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Equal(t, "failed", body["status"])
}

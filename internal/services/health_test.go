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

package services_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

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

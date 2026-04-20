package apirouter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func newAuditTest(t *testing.T, opts ...apiTestOption) (*apiTest, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	// When auditLogger is nil, Audit() falls through to the main logger.
	logger := &logging.Logger{Logger: otelzap.New(zap.New(core))}
	opts = append(opts, withLogger(logger))
	return newAPITest(t, opts...), logs
}

func findAuditLog(logs *observer.ObservedLogs, msg string) *observer.LoggedEntry {
	for _, entry := range logs.All() {
		if entry.Message == msg {
			return &entry
		}
	}
	return nil
}

func assertAuditField(t *testing.T, entry *observer.LoggedEntry, key, expected string) {
	t.Helper()
	for _, f := range entry.Context {
		if f.Key == key {
			assert.Equal(t, expected, f.String, "audit field %s", key)
			return
		}
	}
	t.Errorf("audit field %q not found in log entry", key)
}

func TestAuditLog_Tenant(t *testing.T) {
	t.Run("tenant created", func(t *testing.T) {
		h, logs := newAuditTest(t)

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusCreated, resp.Code)
		entry := findAuditLog(logs, "tenant created")
		require.NotNil(t, entry, "expected 'tenant created' audit log")
		assertAuditField(t, entry, "tenant_id", "t1")
	})

	t.Run("tenant updated", func(t *testing.T) {
		h, logs := newAuditTest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		req := h.jsonReq(http.MethodPut, "/api/v1/tenants/t1", map[string]any{
			"metadata": map[string]string{"env": "prod"},
		})
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		entry := findAuditLog(logs, "tenant updated")
		require.NotNil(t, entry, "expected 'tenant updated' audit log")
		assertAuditField(t, entry, "tenant_id", "t1")
	})

	t.Run("tenant deleted", func(t *testing.T) {
		h, logs := newAuditTest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t1", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		entry := findAuditLog(logs, "tenant deleted")
		require.NotNil(t, entry, "expected 'tenant deleted' audit log")
		assertAuditField(t, entry, "tenant_id", "t1")
	})
}

func TestAuditLog_Destination(t *testing.T) {
	t.Run("destination created", func(t *testing.T) {
		h, logs := newAuditTest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", validDestination())
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusCreated, resp.Code)
		entry := findAuditLog(logs, "destination created")
		require.NotNil(t, entry, "expected 'destination created' audit log")
		assertAuditField(t, entry, "tenant_id", "t1")
		assertAuditField(t, entry, "destination_type", "webhook")
	})

	t.Run("destination updated", func(t *testing.T) {
		h, logs := newAuditTest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.CreateDestination(t.Context(), df.Any(
			df.WithTenantID("t1"), df.WithID("d1"), df.WithTopics([]string{"user.created"}),
		))

		req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
			"topics": []string{"user.deleted"},
		})
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		entry := findAuditLog(logs, "destination updated")
		require.NotNil(t, entry, "expected 'destination updated' audit log")
		assertAuditField(t, entry, "tenant_id", "t1")
		assertAuditField(t, entry, "destination_id", "d1")
	})

	t.Run("destination deleted", func(t *testing.T) {
		h, logs := newAuditTest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithTenantID("t1"), df.WithID("d1")))

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t1/destinations/d1", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		entry := findAuditLog(logs, "destination deleted")
		require.NotNil(t, entry, "expected 'destination deleted' audit log")
		assertAuditField(t, entry, "tenant_id", "t1")
		assertAuditField(t, entry, "destination_id", "d1")
	})

	t.Run("destination disabled", func(t *testing.T) {
		h, logs := newAuditTest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithTenantID("t1"), df.WithID("d1")))

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		entry := findAuditLog(logs, "destination disabled")
		require.NotNil(t, entry, "expected 'destination disabled' audit log")
		assertAuditField(t, entry, "tenant_id", "t1")
		assertAuditField(t, entry, "destination_id", "d1")
	})

	t.Run("destination enabled", func(t *testing.T) {
		h, logs := newAuditTest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithTenantID("t1"), df.WithID("d1"), df.WithDisabledAt(time.Now())))

		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/enable", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		entry := findAuditLog(logs, "destination enabled")
		require.NotNil(t, entry, "expected 'destination enabled' audit log")
		assertAuditField(t, entry, "tenant_id", "t1")
		assertAuditField(t, entry, "destination_id", "d1")
	})

	t.Run("no audit log when disable is no-op", func(t *testing.T) {
		h, logs := newAuditTest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithTenantID("t1"), df.WithID("d1"), df.WithDisabledAt(time.Now())))

		// Disabling an already-disabled destination should not emit audit log
		req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		entry := findAuditLog(logs, "destination disabled")
		assert.Nil(t, entry, "should not emit audit log for no-op disable")
	})
}

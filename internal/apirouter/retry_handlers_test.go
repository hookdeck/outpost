package apirouter_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_Retry(t *testing.T) {
	// setup creates a standard test harness with a tenant, destination, and event
	// that are all compatible for a successful retry.
	setup := func(t *testing.T, opts ...apiTestOption) *apiTest {
		t.Helper()
		h := newAPITest(t, opts...)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"*"})))
		e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e, Attempt: attemptForEvent(e)},
		}))
		return h
	}

	t.Run("Auth", func(t *testing.T) {
		t.Run("no auth returns 401", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(req)

			require.Equal(t, http.StatusUnauthorized, resp.Code)
		})

		t.Run("api key succeeds", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
		})

		t.Run("jwt own tenant succeeds", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusAccepted, resp.Code)
		})
	})

	t.Run("Validation", func(t *testing.T) {
		t.Run("no body returns 400", func(t *testing.T) {
			h := setup(t)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/retry", nil)
			req.Header.Set("Content-Type", "application/json")
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusBadRequest, resp.Code)
		})

		t.Run("empty JSON returns 400", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusBadRequest, resp.Code)
		})

		t.Run("missing event_id returns 400", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusBadRequest, resp.Code)
		})

		t.Run("missing destination_id returns 400", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id": "e1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusBadRequest, resp.Code)
		})
	})

	t.Run("Event lookup", func(t *testing.T) {
		t.Run("event not found returns 404", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "nonexistent",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})
	})

	t.Run("Tenant isolation", func(t *testing.T) {
		t.Run("jwt other tenant event returns 404", func(t *testing.T) {
			h := newAPITest(t)
			// Create two tenants
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
			dest := df.Any(df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}))
			h.tenantStore.UpsertDestination(t.Context(), dest)
			// Event belongs to t1
			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e)},
			}))

			// JWT for t2 tries to retry t1's event
			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withJWT(req, "t2"))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("api key can access any tenant event", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			dest := df.Any(df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}))
			h.tenantStore.UpsertDestination(t.Context(), dest)
			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e)},
			}))

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
		})
	})

	t.Run("Destination checks", func(t *testing.T) {
		t.Run("destination not found returns 404", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "nonexistent",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("disabled destination returns 400", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			now := time.Now()
			dest := df.Any(df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}), df.WithDisabledAt(now))
			h.tenantStore.UpsertDestination(t.Context(), dest)
			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e)},
			}))

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusBadRequest, resp.Code)
		})

		t.Run("topic mismatch returns 400", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			// Destination only accepts "user.deleted"
			dest := df.Any(df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"user.deleted"}))
			h.tenantStore.UpsertDestination(t.Context(), dest)
			// Event has topic "user.created"
			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e)},
			}))

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusBadRequest, resp.Code)
		})

		t.Run("wildcard destination matches any topic", func(t *testing.T) {
			h := setup(t) // setup uses topics: ["*"]

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
		})
	})

	t.Run("Delivery task", func(t *testing.T) {
		t.Run("queues manual delivery task", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.deliveryPub.calls, 1)

			task := h.deliveryPub.calls[0]
			assert.True(t, task.Manual)
			assert.Equal(t, "e1", task.Event.ID)
			assert.Equal(t, "t1", task.Event.TenantID)
			assert.Equal(t, "d1", task.DestinationID)
		})

		t.Run("returns success body", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)

			var body map[string]any
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			assert.Equal(t, true, body["success"])
		})

		t.Run("publisher error returns 500", func(t *testing.T) {
			h := setup(t)
			h.deliveryPub.err = errors.New("queue unavailable")

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusInternalServerError, resp.Code)
		})
	})
}

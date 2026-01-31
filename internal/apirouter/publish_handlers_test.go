package apirouter_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_Publish(t *testing.T) {
	t.Run("Auth", func(t *testing.T) {
		t.Run("no auth returns 401", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(req)

			require.Equal(t, http.StatusUnauthorized, resp.Code)
		})

		t.Run("jwt returns 401", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusUnauthorized, resp.Code)
		})

		t.Run("api key succeeds", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
		})
	})

	t.Run("Validation", func(t *testing.T) {
		t.Run("empty JSON returns 422", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})

		t.Run("missing tenant_id returns 422", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"topic": "user.created",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})

		t.Run("no body returns 400", func(t *testing.T) {
			h := newAPITest(t)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/publish", nil)
			req.Header.Set("Content-Type", "application/json")
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusBadRequest, resp.Code)
		})
	})

	t.Run("Error mapping", func(t *testing.T) {
		t.Run("idempotency conflict returns 409", func(t *testing.T) {
			h := newAPITest(t)
			h.eventHandler.err = idempotence.ErrConflict

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusConflict, resp.Code)
		})

		t.Run("required topic returns 422 with detail", func(t *testing.T) {
			h := newAPITest(t)
			h.eventHandler.err = publishmq.ErrRequiredTopic

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)

			var body map[string]any
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			data, ok := body["data"].([]any)
			require.True(t, ok)
			assert.Contains(t, data, "topic is required")
		})

		t.Run("invalid topic returns 422 with detail", func(t *testing.T) {
			h := newAPITest(t)
			h.eventHandler.err = publishmq.ErrInvalidTopic

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)

			var body map[string]any
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			data, ok := body["data"].([]any)
			require.True(t, ok)
			assert.Contains(t, data, "topic is invalid")
		})

		t.Run("internal error returns 500", func(t *testing.T) {
			h := newAPITest(t)
			h.eventHandler.err = errors.New("database error")

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusInternalServerError, resp.Code)
		})
	})

	t.Run("Success", func(t *testing.T) {
		t.Run("returns event ID", func(t *testing.T) {
			h := newAPITest(t)
			h.eventHandler.result = &publishmq.HandleResult{EventID: "evt-123"}

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)

			var result publishmq.HandleResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Equal(t, "evt-123", result.EventID)
			assert.False(t, result.Duplicate)
		})

		t.Run("returns duplicate flag", func(t *testing.T) {
			h := newAPITest(t)
			h.eventHandler.result = &publishmq.HandleResult{EventID: "evt-123", Duplicate: true}

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)

			var result publishmq.HandleResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.True(t, result.Duplicate)
		})
	})

	t.Run("Input defaults", func(t *testing.T) {
		t.Run("auto-generates ID when omitted", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.eventHandler.calls, 1)
			assert.NotEmpty(t, h.eventHandler.calls[0].ID)
		})

		t.Run("uses explicit ID", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"id":        "custom-id",
				"tenant_id": "t1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.eventHandler.calls, 1)
			assert.Equal(t, "custom-id", h.eventHandler.calls[0].ID)
		})

		t.Run("defaults time to now", func(t *testing.T) {
			h := newAPITest(t)
			before := time.Now()

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(h.withAPIKey(req))
			after := time.Now()

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.eventHandler.calls, 1)
			eventTime := h.eventHandler.calls[0].Time
			assert.False(t, eventTime.Before(before))
			assert.False(t, eventTime.After(after))
		})

		t.Run("uses explicit time", func(t *testing.T) {
			h := newAPITest(t)
			explicit := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
				"time":      explicit.Format(time.RFC3339),
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.eventHandler.calls, 1)
			assert.True(t, h.eventHandler.calls[0].Time.Equal(explicit))
		})

		t.Run("defaults eligible_for_retry to true", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.eventHandler.calls, 1)
			assert.True(t, h.eventHandler.calls[0].EligibleForRetry)
		})

		t.Run("eligible_for_retry false", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id":          "t1",
				"eligible_for_retry": false,
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.eventHandler.calls, 1)
			assert.False(t, h.eventHandler.calls[0].EligibleForRetry)
		})

		t.Run("preserves tenant_id", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "my-tenant",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.eventHandler.calls, 1)
			assert.Equal(t, "my-tenant", h.eventHandler.calls[0].TenantID)
		})

		t.Run("preserves topic", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
				"topic":     "user.created",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.eventHandler.calls, 1)
			assert.Equal(t, "user.created", h.eventHandler.calls[0].Topic)
		})

		t.Run("preserves destination_id", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id":      "t1",
				"destination_id": "dest-1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.eventHandler.calls, 1)
			assert.Equal(t, "dest-1", h.eventHandler.calls[0].DestinationID)
		})

		t.Run("preserves metadata", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
				"metadata":  map[string]string{"env": "prod"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.eventHandler.calls, 1)
			assert.Equal(t, models.Metadata{"env": "prod"}, h.eventHandler.calls[0].Metadata)
		})

		t.Run("preserves data", func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/publish", map[string]any{
				"tenant_id": "t1",
				"data":      map[string]any{"foo": "bar"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.eventHandler.calls, 1)
			assert.Equal(t, "bar", h.eventHandler.calls[0].Data["foo"])
		})
	})
}

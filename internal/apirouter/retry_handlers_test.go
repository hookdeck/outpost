package apirouter_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unscopedLogStore ignores the tenant filter on attempt and event lookups.
type unscopedLogStore struct {
	logstore.LogStore
}

func (s *unscopedLogStore) ListAttempt(ctx context.Context, req logstore.ListAttemptRequest) (logstore.ListAttemptResponse, error) {
	req.TenantIDs = nil
	return s.LogStore.ListAttempt(ctx, req)
}

func (s *unscopedLogStore) RetrieveEvent(ctx context.Context, req logstore.RetrieveEventRequest) (*models.Event, error) {
	req.TenantID = ""
	return s.LogStore.RetrieveEvent(ctx, req)
}

// retrieveDestinationErrorStore fails every destination lookup.
type retrieveDestinationErrorStore struct {
	tenantstore.TenantStore
}

func (s *retrieveDestinationErrorStore) RetrieveDestination(context.Context, string, string) (*models.Destination, error) {
	return nil, errors.New("store unavailable")
}

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
			{Event: e, Attempt: attemptForEvent(e, af.WithDestinationID("d1"))},
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
		t.Run("no body returns 422", func(t *testing.T) {
			h := setup(t)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/retry", nil)
			req.Header.Set("Content-Type", "application/json")
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})

		t.Run("empty JSON returns 422", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})

		t.Run("missing event_id returns 422", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})

		t.Run("missing destination_id returns 422", func(t *testing.T) {
			h := setup(t)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id": "e1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
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

			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "event not found")
		})
	})

	t.Run("Tenant isolation", func(t *testing.T) {
		// seed creates two tenants and an event of t1 delivered to t1's destination.
		seed := func(t *testing.T, h *apiTest) {
			t.Helper()
			require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
			require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2"))))
			dest := df.Any(df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}))
			require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), dest))
			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e, af.WithDestinationID("d1"))},
			}))
		}

		t.Run("jwt other tenant event returns 404", func(t *testing.T) {
			h := newAPITest(t)
			seed(t, h)

			// JWT for t2 tries to retry t1's event
			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withJWT(req, "t2"))

			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "event not found")
			assert.Empty(t, h.deliveryPub.calls)
		})

		t.Run("jwt other tenant event returns 404 when the log store returns it", func(t *testing.T) {
			h := newAPITest(t, withLogStore(&unscopedLogStore{logstore.NewMemLogStore()}))
			seed(t, h)

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withJWT(req, "t2"))

			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "event not found")
			assert.Empty(t, h.deliveryPub.calls)
		})

		t.Run("api key can access any tenant event", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			dest := df.Any(df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}))
			h.tenantStore.UpsertDestination(t.Context(), dest)
			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e, af.WithDestinationID("d1"))},
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
			// The event was delivered to a destination the tenant store does not have.
			e := ef.AnyPointer(ef.WithID("e2"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e, af.WithDestinationID("gone"))},
			}))

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e2",
				"destination_id": "gone",
			})
			resp := h.do(h.withAPIKey(req))

			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "destination not found")
		})

		t.Run("deleted destination returns 404", func(t *testing.T) {
			h := setup(t)
			require.NoError(t, h.tenantStore.DeleteDestination(t.Context(), "t1", "d1"))

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "destination not found")
			assert.Empty(t, h.deliveryPub.calls)
		})

		t.Run("destination store error returns 500", func(t *testing.T) {
			h := setup(t, withTenantStore(&retrieveDestinationErrorStore{tenantstore.NewMemTenantStore()}))

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			testutil.RequireErrorResponse(t, resp, http.StatusInternalServerError, "internal server error")
		})

		t.Run("disabled destination returns 400", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			now := time.Now()
			dest := df.Any(df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}), df.WithDisabledAt(now))
			h.tenantStore.UpsertDestination(t.Context(), dest)
			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e, af.WithDestinationID("d1"))},
			}))

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			testutil.RequireErrorResponse(t, resp, http.StatusBadRequest, "Destination is disabled")
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
				{Event: e, Attempt: attemptForEvent(e, af.WithDestinationID("d1"))},
			}))

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			testutil.RequireErrorResponse(t, resp, http.StatusBadRequest, "destination does not match event")
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

		t.Run("embedded wildcard is ignored when disabled", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"user.*"}),
			))
			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e, af.WithDestinationID("d1"))},
			}))

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))
			testutil.RequireErrorResponse(t, resp, http.StatusBadRequest, "destination does not match event")
		})
	})

	// The event exists, but the destination in the request has no attempt for it.
	t.Run("No attempt for the destination", func(t *testing.T) {
		retry := func(h *apiTest, eventID, destinationID string) *http.Request {
			return h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       eventID,
				"destination_id": destinationID,
			})
		}

		t.Run("unknown destination returns 404 destination not found", func(t *testing.T) {
			h := setup(t)

			resp := h.do(h.withAPIKey(retry(h, "e1", "nonexistent")))

			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "destination not found")
			assert.Empty(t, h.deliveryPub.calls)
		})

		t.Run("unknown event and unknown destination returns 404 event not found", func(t *testing.T) {
			h := setup(t)

			resp := h.do(h.withAPIKey(retry(h, "nonexistent", "nonexistent")))

			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "event not found")
		})

		t.Run("destination of another tenant returns 404 destination not found", func(t *testing.T) {
			h := setup(t)
			require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2"))))
			require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d2"), df.WithTenantID("t2"), df.WithTopics([]string{"*"}))))
			require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d2-disabled"), df.WithTenantID("t2"), df.WithTopics([]string{"*"}), df.WithDisabledAt(time.Now()))))
			require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d2-mismatch"), df.WithTenantID("t2"), df.WithTopics([]string{"user.deleted"}))))

			// Whatever its state, another tenant's destination reads as an unknown one.
			for _, destinationID := range []string{"d2", "d2-disabled", "d2-mismatch"} {
				for _, withAuth := range []func(*http.Request) *http.Request{
					h.withAPIKey,
					func(r *http.Request) *http.Request { return h.withJWT(r, "t1") },
				} {
					resp := h.do(withAuth(retry(h, "e1", destinationID)))

					testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "destination not found")
				}
			}
			assert.Empty(t, h.deliveryPub.calls)
		})

		t.Run("deleted destination returns 404 destination not found", func(t *testing.T) {
			h := setup(t)
			require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d2"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}))))
			require.NoError(t, h.tenantStore.DeleteDestination(t.Context(), "t1", "d2"))

			resp := h.do(h.withAPIKey(retry(h, "e1", "d2")))

			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "destination not found")
		})

		t.Run("disabled destination returns 400", func(t *testing.T) {
			h := setup(t)
			require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d2"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}), df.WithDisabledAt(time.Now()))))

			resp := h.do(h.withAPIKey(retry(h, "e1", "d2")))

			testutil.RequireErrorResponse(t, resp, http.StatusBadRequest, "Destination is disabled")
			var body struct {
				Data map[string]string `json:"data"`
			}
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			assert.Equal(t, "destination_disabled", body.Data["error"])
		})

		t.Run("topic mismatch returns 400", func(t *testing.T) {
			h := setup(t)
			require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d2"), df.WithTenantID("t1"), df.WithTopics([]string{"user.deleted"}))))

			resp := h.do(h.withAPIKey(retry(h, "e1", "d2")))

			testutil.RequireErrorResponse(t, resp, http.StatusBadRequest, "destination does not match event")
		})

		t.Run("matching destination returns 400 and queues nothing", func(t *testing.T) {
			h := setup(t)
			require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d2"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}))))

			for _, withAuth := range []func(*http.Request) *http.Request{
				h.withAPIKey,
				func(r *http.Request) *http.Request { return h.withJWT(r, "t1") },
			} {
				resp := h.do(withAuth(retry(h, "e1", "d2")))

				testutil.RequireErrorResponse(t, resp, http.StatusBadRequest, "event has no attempt for this destination")
			}
			assert.Empty(t, h.deliveryPub.calls)
		})

		t.Run("jwt other tenant event returns 404 event not found", func(t *testing.T) {
			h := setup(t)
			require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2"))))
			require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d2"), df.WithTenantID("t2"), df.WithTopics([]string{"*"}))))

			// t2's own destination and an unknown one: neither reveals t1's event.
			for _, destinationID := range []string{"d2", "nonexistent"} {
				resp := h.do(h.withJWT(retry(h, "e1", destinationID), "t2"))

				testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "event not found")
			}
			assert.Empty(t, h.deliveryPub.calls)
		})

		t.Run("jwt other tenant event returns 404 when the log store returns it", func(t *testing.T) {
			h := setup(t, withLogStore(&unscopedLogStore{logstore.NewMemLogStore()}))
			require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2"))))
			require.NoError(t, h.tenantStore.UpsertDestination(t.Context(), df.Any(df.WithID("d2"), df.WithTenantID("t2"), df.WithTopics([]string{"*"}))))

			resp := h.do(h.withJWT(retry(h, "e1", "d2"), "t2"))

			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "event not found")
			assert.Empty(t, h.deliveryPub.calls)
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
			assert.Equal(t, 2, task.Attempt, "should derive attempt_number=2 from 1 prior attempt")
		})

		t.Run("does not look the event up when an attempt exists", func(t *testing.T) {
			h := setup(t, withLogStore(&failingLogStore{LogStore: logstore.NewMemLogStore(), fail: "RetrieveEvent"}))

			req := h.jsonReq(http.MethodPost, "/api/v1/retry", map[string]any{
				"event_id":       "e1",
				"destination_id": "d1",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusAccepted, resp.Code)
			require.Len(t, h.deliveryPub.calls, 1)
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

			testutil.RequireErrorResponse(t, resp, http.StatusInternalServerError, "internal server error")
		})
	})
}

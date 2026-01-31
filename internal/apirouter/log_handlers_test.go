package apirouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// attemptForEvent creates an attempt that references the given event.
func attemptForEvent(event *models.Event, opts ...func(*models.Attempt)) *models.Attempt {
	return af.AnyPointer(append([]func(*models.Attempt){
		af.WithEventID(event.ID),
		af.WithTenantID(event.TenantID),
		af.WithDestinationID(event.DestinationID),
	}, opts...)...)
}

func TestAPI_Events(t *testing.T) {
	t.Run("List", func(t *testing.T) {
		t.Run("api key returns all events", func(t *testing.T) {
			h := newAPITest(t)

			e1 := ef.AnyPointer(ef.WithTenantID("t1"))
			e2 := ef.AnyPointer(ef.WithTenantID("t2"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: attemptForEvent(e1)},
				{Event: e2, Attempt: attemptForEvent(e2)},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var result apirouter.EventPaginatedResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Len(t, result.Models, 2)
		})

		t.Run("api key with tenant_id filter", func(t *testing.T) {
			h := newAPITest(t)

			e1 := ef.AnyPointer(ef.WithTenantID("t1"))
			e2 := ef.AnyPointer(ef.WithTenantID("t2"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: attemptForEvent(e1)},
				{Event: e2, Attempt: attemptForEvent(e2)},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events?tenant_id=t1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var result apirouter.EventPaginatedResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Len(t, result.Models, 1)
			assert.Equal(t, e1.ID, result.Models[0].ID)
		})

		t.Run("api key with topic filter", func(t *testing.T) {
			h := newAPITest(t)

			e1 := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			e2 := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithTopic("user.updated"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: attemptForEvent(e1)},
				{Event: e2, Attempt: attemptForEvent(e2)},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events?topic=user.created", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var result apirouter.EventPaginatedResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Len(t, result.Models, 1)
			assert.Equal(t, "user.created", result.Models[0].Topic)
		})

		t.Run("default pagination metadata", func(t *testing.T) {
			h := newAPITest(t)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var result apirouter.EventPaginatedResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Equal(t, "time", result.Pagination.OrderBy)
			assert.Equal(t, "desc", result.Pagination.Dir)
			assert.Equal(t, 100, result.Pagination.Limit)
			assert.Nil(t, result.Pagination.Next)
			assert.Nil(t, result.Pagination.Prev)
		})

		t.Run("jwt returns own tenant events", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			e1 := ef.AnyPointer(ef.WithTenantID("t1"))
			e2 := ef.AnyPointer(ef.WithTenantID("t2"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: attemptForEvent(e1)},
				{Event: e2, Attempt: attemptForEvent(e2)},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)

			var result apirouter.EventPaginatedResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Len(t, result.Models, 1)
			assert.Equal(t, e1.ID, result.Models[0].ID)
		})

		t.Run("jwt with matching tenant_id returns 200", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			e1 := ef.AnyPointer(ef.WithTenantID("t1"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: attemptForEvent(e1)},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events?tenant_id=t1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)

			var result apirouter.EventPaginatedResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Len(t, result.Models, 1)
		})

		t.Run("jwt with mismatched tenant_id returns 403", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events?tenant_id=t2", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusForbidden, resp.Code)
		})
	})

	t.Run("Retrieve", func(t *testing.T) {
		t.Run("api key returns event", func(t *testing.T) {
			h := newAPITest(t)

			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e)},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events/e1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var event apirouter.APIEvent
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &event))
			assert.Equal(t, "e1", event.ID)
		})

		t.Run("nonexistent event returns 404", func(t *testing.T) {
			h := newAPITest(t)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events/nope", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("jwt returns own event", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e)},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events/e1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)

			var event apirouter.APIEvent
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &event))
			assert.Equal(t, "e1", event.ID)
		})

		t.Run("jwt other tenant event returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t2"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e)},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/events/e1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		h := newAPITest(t)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
		resp := h.do(req)

		require.Equal(t, http.StatusUnauthorized, resp.Code)
	})
}

func TestAPI_Attempts(t *testing.T) {
	t.Run("List", func(t *testing.T) {
		t.Run("api key returns all attempts", func(t *testing.T) {
			h := newAPITest(t)

			e1 := ef.AnyPointer(ef.WithTenantID("t1"))
			e2 := ef.AnyPointer(ef.WithTenantID("t2"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: attemptForEvent(e1)},
				{Event: e2, Attempt: attemptForEvent(e2)},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var result apirouter.AttemptPaginatedResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Len(t, result.Models, 2)
		})

		t.Run("api key with tenant_id filter", func(t *testing.T) {
			h := newAPITest(t)

			e1 := ef.AnyPointer(ef.WithTenantID("t1"))
			e2 := ef.AnyPointer(ef.WithTenantID("t2"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: attemptForEvent(e1)},
				{Event: e2, Attempt: attemptForEvent(e2)},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?tenant_id=t1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var result apirouter.AttemptPaginatedResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Len(t, result.Models, 1)
		})

		t.Run("jwt returns own tenant attempts", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			e1 := ef.AnyPointer(ef.WithTenantID("t1"))
			e2 := ef.AnyPointer(ef.WithTenantID("t2"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: attemptForEvent(e1)},
				{Event: e2, Attempt: attemptForEvent(e2)},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)

			var result apirouter.AttemptPaginatedResult
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
			assert.Len(t, result.Models, 1)
		})

		t.Run("jwt with mismatched tenant_id returns 403", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?tenant_id=t2", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusForbidden, resp.Code)
		})
	})

	t.Run("Retrieve", func(t *testing.T) {
		t.Run("api key returns attempt", func(t *testing.T) {
			h := newAPITest(t)

			e := ef.AnyPointer(ef.WithTenantID("t1"))
			a := attemptForEvent(e, af.WithID("a1"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: a},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts/a1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var attempt apirouter.APIAttempt
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &attempt))
			assert.Equal(t, "a1", attempt.ID)
		})

		t.Run("nonexistent attempt returns 404", func(t *testing.T) {
			h := newAPITest(t)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts/nope", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("jwt returns own attempt", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			e := ef.AnyPointer(ef.WithTenantID("t1"))
			a := attemptForEvent(e, af.WithID("a1"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: a},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts/a1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)

			var attempt apirouter.APIAttempt
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &attempt))
			assert.Equal(t, "a1", attempt.ID)
		})

		t.Run("jwt other tenant attempt returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			e := ef.AnyPointer(ef.WithTenantID("t2"))
			a := attemptForEvent(e, af.WithID("a1"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: a},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts/a1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("include event expands event summary", func(t *testing.T) {
			h := newAPITest(t)

			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			a := attemptForEvent(e, af.WithID("a1"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: a},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts/a1?include=event", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var raw map[string]any
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &raw))

			// With include=event, the event field is an object (not just an ID)
			eventMap, ok := raw["event"].(map[string]any)
			require.True(t, ok, "event should be an object when include=event")
			assert.Equal(t, "e1", eventMap["id"])
			assert.Equal(t, "user.created", eventMap["topic"])
			// Summary does not include data
			_, hasData := eventMap["data"]
			assert.False(t, hasData)
		})

		t.Run("include event.data expands event with data", func(t *testing.T) {
			h := newAPITest(t)

			e := ef.AnyPointer(
				ef.WithID("e1"), ef.WithTenantID("t1"),
				ef.WithData(map[string]any{"key": "val"}),
			)
			a := attemptForEvent(e, af.WithID("a1"))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: a},
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts/a1?include=event.data", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var raw map[string]any
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &raw))

			eventMap, ok := raw["event"].(map[string]any)
			require.True(t, ok, "event should be an object when include=event.data")
			assert.Equal(t, "e1", eventMap["id"])
			dataMap, ok := eventMap["data"].(map[string]any)
			require.True(t, ok, "event.data should be present")
			assert.Equal(t, "val", dataMap["key"])
		})
	})

	t.Run("DestinationAttempts", func(t *testing.T) {
		t.Run("List", func(t *testing.T) {
			t.Run("api key returns attempts for destination", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

				e1 := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithDestinationID("d1"))
				e2 := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithDestinationID("d2"))
				require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
					{Event: e1, Attempt: attemptForEvent(e1)},
					{Event: e2, Attempt: attemptForEvent(e2)},
				}))

				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1/attempts", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				assert.Len(t, result.Models, 1)
				assert.Equal(t, "d1", result.Models[0].Destination)
			})

			t.Run("excludes attempts from other tenants same destination", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

				e1 := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithDestinationID("d1"))
				e2 := ef.AnyPointer(ef.WithTenantID("t2"), ef.WithDestinationID("d1"))
				require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
					{Event: e1, Attempt: attemptForEvent(e1)},
					{Event: e2, Attempt: attemptForEvent(e2)},
				}))

				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1/attempts", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				assert.Len(t, result.Models, 1)
			})

			t.Run("jwt returns attempts for own destination", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

				e := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithDestinationID("d1"))
				require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
					{Event: e, Attempt: attemptForEvent(e)},
				}))

				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1/attempts", nil)
				resp := h.do(h.withJWT(req, "t1"))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				assert.Len(t, result.Models, 1)
			})

			t.Run("jwt other tenant returns 403", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t2/destinations/d1/attempts", nil)
				resp := h.do(h.withJWT(req, "t1"))

				require.Equal(t, http.StatusForbidden, resp.Code)
			})
		})

		t.Run("Retrieve", func(t *testing.T) {
			t.Run("api key retrieves specific attempt", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

				e := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithDestinationID("d1"))
				a := attemptForEvent(e, af.WithID("a1"))
				require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
					{Event: e, Attempt: a},
				}))

				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1/attempts/a1", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var attempt apirouter.APIAttempt
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &attempt))
				assert.Equal(t, "a1", attempt.ID)
				assert.Equal(t, "d1", attempt.Destination)
			})

			t.Run("attempt from different destination still returned", func(t *testing.T) {
				// RetrieveAttempt filters by tenant only, not by destination in path.
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d2"), df.WithTenantID("t1")))

				e := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithDestinationID("d2"))
				a := attemptForEvent(e, af.WithID("a1"))
				require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
					{Event: e, Attempt: a},
				}))

				// Request via d1's path, but attempt belongs to d2
				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1/attempts/a1", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var attempt apirouter.APIAttempt
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &attempt))
				assert.Equal(t, "d2", attempt.Destination)
			})

			t.Run("jwt other tenant returns 403", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

				e := ef.AnyPointer(ef.WithTenantID("t2"), ef.WithDestinationID("d1"))
				a := attemptForEvent(e, af.WithID("a1"))
				require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
					{Event: e, Attempt: a},
				}))

				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t2/destinations/d1/attempts/a1", nil)
				resp := h.do(h.withJWT(req, "t1"))

				require.Equal(t, http.StatusForbidden, resp.Code)
			})
		})
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		h := newAPITest(t)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts", nil)
		resp := h.do(req)

		require.Equal(t, http.StatusUnauthorized, resp.Code)
	})
}

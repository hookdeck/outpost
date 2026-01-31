package apirouter_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

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

		// Pagination, filtering, and validation are tested comprehensively under
		// TestAPI_Attempts since attempts are the primary query surface. Events
		// share the same underlying pagination/filter machinery (ParseCursors,
		// ParseDir, ParseOrderBy, ParseDateFilter) so we keep a lighter smoke
		// suite here to confirm the wiring without duplicating every scenario.

		t.Run("Pagination", func(t *testing.T) {
			h := newAPITest(t)

			now := time.Now()
			e1 := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTime(now.Add(-2*time.Second)))
			e2 := ef.AnyPointer(ef.WithID("e2"), ef.WithTenantID("t1"), ef.WithTime(now.Add(-1*time.Second)))
			e3 := ef.AnyPointer(ef.WithID("e3"), ef.WithTenantID("t1"), ef.WithTime(now))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: attemptForEvent(e1)},
				{Event: e2, Attempt: attemptForEvent(e2)},
				{Event: e3, Attempt: attemptForEvent(e3)},
			}))

			t.Run("forward pagination returns pages in order", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=1&dir=desc", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "e3", result.Models[0].ID)
				assert.NotNil(t, result.Pagination.Next)
				assert.Nil(t, result.Pagination.Prev)
			})

			t.Run("next cursor returns second page", func(t *testing.T) {
				// Get first page
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=1&dir=desc", nil)
				resp := h.do(h.withAPIKey(req))
				var page1 apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page1))
				require.NotNil(t, page1.Pagination.Next)

				// Get second page
				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/events?limit=1&next=%s", *page1.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var page2 apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page2))
				require.Len(t, page2.Models, 1)
				assert.Equal(t, "e2", page2.Models[0].ID)
				assert.NotNil(t, page2.Pagination.Next)
				assert.NotNil(t, page2.Pagination.Prev)
			})

			t.Run("last page has no next cursor", func(t *testing.T) {
				// Get first page
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=1&dir=desc", nil)
				resp := h.do(h.withAPIKey(req))
				var page1 apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page1))

				// Get second page
				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/events?limit=1&next=%s", *page1.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))
				var page2 apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page2))

				// Get third (last) page
				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/events?limit=1&next=%s", *page2.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var page3 apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page3))
				require.Len(t, page3.Models, 1)
				assert.Equal(t, "e1", page3.Models[0].ID)
				assert.Nil(t, page3.Pagination.Next)
				assert.NotNil(t, page3.Pagination.Prev)
			})

			t.Run("prev cursor returns previous page", func(t *testing.T) {
				// Navigate to last page
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=1&dir=desc", nil)
				resp := h.do(h.withAPIKey(req))
				var page1 apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page1))

				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/events?limit=1&next=%s", *page1.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))
				var page2 apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page2))

				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/events?limit=1&next=%s", *page2.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))
				var page3 apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page3))
				require.NotNil(t, page3.Pagination.Prev)

				// Go back
				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/events?limit=1&prev=%s", *page3.Pagination.Prev), nil)
				resp = h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var prevPage apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &prevPage))
				require.Len(t, prevPage.Models, 1)
				assert.Equal(t, "e2", prevPage.Models[0].ID)
			})

			t.Run("dir asc reverses order", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=1&dir=asc", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "e1", result.Models[0].ID)
			})

			t.Run("limit caps results", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?limit=2", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				assert.Len(t, result.Models, 2)
				assert.NotNil(t, result.Pagination.Next)
			})
		})

		t.Run("Filtering", func(t *testing.T) {
			h := newAPITest(t)

			now := time.Now()
			e1 := ef.AnyPointer(
				ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithDestinationID("d1"),
				ef.WithTopic("user.created"), ef.WithTime(now.Add(-2*time.Hour)),
			)
			e2 := ef.AnyPointer(
				ef.WithID("e2"), ef.WithTenantID("t1"), ef.WithDestinationID("d2"),
				ef.WithTopic("user.updated"), ef.WithTime(now),
			)
			e3 := ef.AnyPointer(
				ef.WithID("e3"), ef.WithTenantID("t2"), ef.WithDestinationID("d3"),
				ef.WithTopic("user.created"), ef.WithTime(now),
			)
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: attemptForEvent(e1)},
				{Event: e2, Attempt: attemptForEvent(e2)},
				{Event: e3, Attempt: attemptForEvent(e3)},
			}))

			t.Run("destination_id filter", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?destination_id=d1", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "e1", result.Models[0].ID)
			})

			t.Run("multiple topics filter", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?topic=user.created&topic=user.updated", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				assert.Len(t, result.Models, 3)
			})

			t.Run("single topic filter", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?topic=user.updated", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "e2", result.Models[0].ID)
			})

			t.Run("time gte filter", func(t *testing.T) {
				cutoff := now.Add(-1 * time.Hour).UTC().Format(time.RFC3339)
				v := url.Values{}
				v.Set("time[gte]", cutoff)
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?"+v.Encode(), nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				assert.Len(t, result.Models, 2)
			})

			t.Run("time lte filter", func(t *testing.T) {
				cutoff := now.Add(-1 * time.Hour).UTC().Format(time.RFC3339)
				v := url.Values{}
				v.Set("time[lte]", cutoff)
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?"+v.Encode(), nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "e1", result.Models[0].ID)
			})

			t.Run("combined filters", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?topic=user.created&tenant_id=t1", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.EventPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "e1", result.Models[0].ID)
			})
		})

		t.Run("Validation", func(t *testing.T) {
			t.Run("invalid dir returns 422", func(t *testing.T) {
				h := newAPITest(t)

				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?dir=sideways", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})

			t.Run("invalid order_by returns 422", func(t *testing.T) {
				h := newAPITest(t)

				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?order_by=name", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})

			t.Run("both next and prev returns 400", func(t *testing.T) {
				h := newAPITest(t)

				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?next=abc&prev=def", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusBadRequest, resp.Code)
			})

			t.Run("invalid date format returns 422", func(t *testing.T) {
				h := newAPITest(t)

				req := httptest.NewRequest(http.MethodGet, "/api/v1/events?time[gte]=not-a-date", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})
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

		t.Run("Pagination", func(t *testing.T) {
			h := newAPITest(t)

			now := time.Now()
			e1 := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTime(now.Add(-2*time.Second)))
			e2 := ef.AnyPointer(ef.WithID("e2"), ef.WithTenantID("t1"), ef.WithTime(now.Add(-1*time.Second)))
			e3 := ef.AnyPointer(ef.WithID("e3"), ef.WithTenantID("t1"), ef.WithTime(now))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: attemptForEvent(e1, af.WithID("a1"), af.WithTime(now.Add(-2*time.Second)))},
				{Event: e2, Attempt: attemptForEvent(e2, af.WithID("a2"), af.WithTime(now.Add(-1*time.Second)))},
				{Event: e3, Attempt: attemptForEvent(e3, af.WithID("a3"), af.WithTime(now))},
			}))

			t.Run("forward pagination returns pages in order", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?limit=1&dir=desc", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "a3", result.Models[0].ID)
				assert.NotNil(t, result.Pagination.Next)
				assert.Nil(t, result.Pagination.Prev)
			})

			t.Run("next cursor returns second page", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?limit=1&dir=desc", nil)
				resp := h.do(h.withAPIKey(req))
				var page1 apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page1))
				require.NotNil(t, page1.Pagination.Next)

				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/attempts?limit=1&next=%s", *page1.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var page2 apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page2))
				require.Len(t, page2.Models, 1)
				assert.Equal(t, "a2", page2.Models[0].ID)
				assert.NotNil(t, page2.Pagination.Next)
				assert.NotNil(t, page2.Pagination.Prev)
			})

			t.Run("last page has no next cursor", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?limit=1&dir=desc", nil)
				resp := h.do(h.withAPIKey(req))
				var page1 apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page1))

				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/attempts?limit=1&next=%s", *page1.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))
				var page2 apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page2))

				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/attempts?limit=1&next=%s", *page2.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var page3 apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page3))
				require.Len(t, page3.Models, 1)
				assert.Equal(t, "a1", page3.Models[0].ID)
				assert.Nil(t, page3.Pagination.Next)
				assert.NotNil(t, page3.Pagination.Prev)
			})

			t.Run("prev cursor returns previous page", func(t *testing.T) {
				// Navigate to last page
				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?limit=1&dir=desc", nil)
				resp := h.do(h.withAPIKey(req))
				var page1 apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page1))

				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/attempts?limit=1&next=%s", *page1.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))
				var page2 apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page2))

				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/attempts?limit=1&next=%s", *page2.Pagination.Next), nil)
				resp = h.do(h.withAPIKey(req))
				var page3 apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page3))
				require.NotNil(t, page3.Pagination.Prev)

				// Go back
				req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/attempts?limit=1&prev=%s", *page3.Pagination.Prev), nil)
				resp = h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var prevPage apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &prevPage))
				require.Len(t, prevPage.Models, 1)
				assert.Equal(t, "a2", prevPage.Models[0].ID)
			})

			t.Run("dir asc reverses order", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?limit=1&dir=asc", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "a1", result.Models[0].ID)
			})

			t.Run("limit caps results", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?limit=2", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				assert.Len(t, result.Models, 2)
				assert.NotNil(t, result.Pagination.Next)
			})
		})

		t.Run("Filtering", func(t *testing.T) {
			h := newAPITest(t)

			now := time.Now()
			e1 := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithDestinationID("d1"), ef.WithTopic("user.created"))
			e2 := ef.AnyPointer(ef.WithID("e2"), ef.WithTenantID("t1"), ef.WithDestinationID("d2"), ef.WithTopic("user.updated"))
			a1 := attemptForEvent(e1, af.WithID("a1"), af.WithStatus("success"), af.WithTime(now.Add(-2*time.Hour)))
			a2 := attemptForEvent(e2, af.WithID("a2"), af.WithStatus("failed"), af.WithTime(now))
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e1, Attempt: a1},
				{Event: e2, Attempt: a2},
			}))

			t.Run("status filter", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?status=success", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "a1", result.Models[0].ID)
			})

			t.Run("event_id filter", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?event_id=e1", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "a1", result.Models[0].ID)
			})

			t.Run("destination_id filter", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?destination_id=d1", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "a1", result.Models[0].ID)
			})

			t.Run("time gte filter", func(t *testing.T) {
				cutoff := now.Add(-1 * time.Hour).UTC().Format(time.RFC3339)
				v := url.Values{}
				v.Set("time[gte]", cutoff)
				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?"+v.Encode(), nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "a2", result.Models[0].ID)
			})

			t.Run("time lte filter", func(t *testing.T) {
				cutoff := now.Add(-1 * time.Hour).UTC().Format(time.RFC3339)
				v := url.Values{}
				v.Set("time[lte]", cutoff)
				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?"+v.Encode(), nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				require.Len(t, result.Models, 1)
				assert.Equal(t, "a1", result.Models[0].ID)
			})
		})

		t.Run("Validation", func(t *testing.T) {
			t.Run("invalid dir returns 422", func(t *testing.T) {
				h := newAPITest(t)

				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?dir=sideways", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})

			t.Run("invalid order_by returns 422", func(t *testing.T) {
				h := newAPITest(t)

				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?order_by=name", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})

			t.Run("both next and prev returns 400", func(t *testing.T) {
				h := newAPITest(t)

				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?next=abc&prev=def", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusBadRequest, resp.Code)
			})

			t.Run("invalid date format returns 422", func(t *testing.T) {
				h := newAPITest(t)

				req := httptest.NewRequest(http.MethodGet, "/api/v1/attempts?time[gte]=not-a-date", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})
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

			t.Run("destination belonging to other tenant returns empty list without leaking data", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

				e := ef.AnyPointer(ef.WithTenantID("t2"), ef.WithDestinationID("d1"))
				require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
					{Event: e, Attempt: attemptForEvent(e)},
				}))

				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1/attempts", nil)
				resp := h.do(h.withAPIKey(req))

				// The handler does not validate destination ownership — it passes the
				// destinationID straight to the log store as a filter alongside the
				// tenant ID. When the destination belongs to another tenant, the query
				// returns no matches because no attempts exist for that (tenant, destination)
				// pair. This means no data leaks, but the API returns 200 with an empty
				// list instead of 404.
				require.Equal(t, http.StatusOK, resp.Code)

				var result apirouter.AttemptPaginatedResult
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				assert.Empty(t, result.Models, "must not leak attempts from other tenants")
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

			t.Run("attempt belonging to different destination returns 404", func(t *testing.T) {
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

				require.Equal(t, http.StatusNotFound, resp.Code)
			})

			t.Run("attempt belonging to other tenant destination returns 404", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d2"), df.WithTenantID("t2")))

				e := ef.AnyPointer(ef.WithTenantID("t2"), ef.WithDestinationID("d2"))
				a := attemptForEvent(e, af.WithID("a1"))
				require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
					{Event: e, Attempt: a},
				}))

				// d1 belongs to t1 (valid), but a1 belongs to d2/t2
				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1/attempts/a1", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusNotFound, resp.Code)
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

			t.Run("destination belonging to other tenant does not leak data", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

				e := ef.AnyPointer(ef.WithTenantID("t2"), ef.WithDestinationID("d1"))
				a := attemptForEvent(e, af.WithID("a1"))
				require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
					{Event: e, Attempt: a},
				}))

				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1/attempts/a1", nil)
				resp := h.do(h.withAPIKey(req))

				// The handler filters by tenant ID, not destination ownership.
				// The attempt belongs to t2 so the tenant filter excludes it — returns
				// 404 with no data leaked.
				require.Equal(t, http.StatusNotFound, resp.Code)
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

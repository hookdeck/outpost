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
	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every list answers a cursor it cannot read with the same 400 and wording.
func TestAPI_InvalidCursorReturns400(t *testing.T) {
	const (
		invalidCursor    = "invalid cursor"
		invalidTimestamp = "invalid cursor: invalid timestamp"
		versionMismatch  = "invalid cursor: cursor version mismatch: expected version 01"
	)

	type cursorCase struct {
		name    string
		cursor  string
		message string
	}

	lists := []struct {
		name     string
		path     string
		resource string // cursor resource of the list
		other    string // cursor resource of another list
	}{
		{"tenants", "/api/v1/tenants", "tnt", "evt"},
		{"events", "/api/v1/events", "evt", "att"},
		{"attempts", "/api/v1/attempts", "att", "evt"},
		{"destination attempts", "/api/v1/tenants/t1/destinations/d1/attempts", "att", "evt"},
	}

	for _, list := range lists {
		t.Run(list.name, func(t *testing.T) {
			cases := []cursorCase{
				{"not base62", "not-a-cursor", invalidCursor},
				{"not a cursor", "abc123", invalidCursor},
				{"cursor of another list", cursor.Encode(list.other, 1, "1700000000000"), invalidCursor},
				{"cursor of another version", cursor.Encode(list.resource, 2, "1700000000000"), versionMismatch},
			}
			if list.resource == "tnt" {
				cases = append(cases, cursorCase{"position that is not a timestamp", cursor.Encode("tnt", 1, "yesterday"), invalidTimestamp})
			} else {
				// The mem log store's position is "{timestamp}_{id}".
				cases = append(cases,
					cursorCase{"empty position", cursor.Encode(list.resource, 1, ""), invalidCursor},
					cursorCase{"position that is one word", cursor.Encode(list.resource, 1, "yesterday"), invalidCursor},
					cursorCase{"position without an id", cursor.Encode(list.resource, 1, "2024-01-15T09:30:00.000000000Z_"), invalidCursor},
					cursorCase{"position of another log store", cursor.Encode(list.resource, 1, "1700000000000::id"), invalidCursor},
					cursorCase{"position with a timestamp that is not a time", cursor.Encode(list.resource, 1, "yesterday_id"), invalidTimestamp},
				)
			}

			for _, tc := range cases {
				for _, param := range []string{"next", "prev"} {
					t.Run(tc.name+" in "+param, func(t *testing.T) {
						h := newAPITest(t)
						require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
						require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1"))))

						query := url.Values{param: {tc.cursor}}
						req := httptest.NewRequest(http.MethodGet, list.path+"?"+query.Encode(), nil)
						resp := h.do(h.withAPIKey(req))

						testutil.RequireErrorResponse(t, resp, http.StatusBadRequest, tc.message)
					})
				}
			}
		})
	}
}

// An empty next or prev is the same as none: the list starts from its first page.
func TestAPI_EmptyCursorIsNoCursor(t *testing.T) {
	paths := []string{
		"/api/v1/tenants",
		"/api/v1/events",
		"/api/v1/attempts",
		"/api/v1/tenants/t1/destinations/d1/attempts",
	}

	for _, path := range paths {
		for _, query := range []string{"next=", "prev=", "next=&prev="} {
			t.Run(path+"?"+query, func(t *testing.T) {
				h := newAPITest(t)
				require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
				require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1"))))
				event := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithDestinationID("d1"))
				require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
					{Event: event, Attempt: attemptForEvent(event)},
				}))

				req := httptest.NewRequest(http.MethodGet, path+"?"+query, nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code, "body: %s", resp.Body.String())
				var result struct {
					Models []json.RawMessage `json:"models"`
				}
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				assert.Len(t, result.Models, 1)
			})
		}
	}
}

// A cursor holds a position and nothing about the query it came from. A cursor
// reused with another sort order or filter is not rejected: the list continues
// from that position with the parameters of the new request.
func TestAPI_CursorOfAnotherQueryListsFromItsPosition(t *testing.T) {
	lists := []struct {
		name string
		path string
		ids  []string // ids of the three records, oldest first
	}{
		{"events", "/api/v1/events", []string{"e1", "e2", "e3"}},
		{"attempts", "/api/v1/attempts", []string{"a1", "a2", "a3"}},
		{"destination attempts", "/api/v1/tenants/t1/destinations/d1/attempts", []string{"a1", "a2", "a3"}},
	}

	for _, list := range lists {
		t.Run(list.name, func(t *testing.T) {
			h := newAPITest(t)
			require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
			require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1"))))

			baseTime := time.Now().Add(-time.Hour).Truncate(time.Second)
			topics := []string{"user.created", "user.updated", "user.updated"}
			var entries []*models.LogEntry
			for i := range 3 {
				at := baseTime.Add(time.Duration(i) * time.Minute)
				event := ef.AnyPointer(ef.WithID(fmt.Sprintf("e%d", i+1)), ef.WithTenantID("t1"), ef.WithDestinationID("d1"), ef.WithTopic(topics[i]), ef.WithTime(at))
				entries = append(entries, &models.LogEntry{
					Event:   event,
					Attempt: attemptForEvent(event, af.WithID(fmt.Sprintf("a%d", i+1)), af.WithTime(at)),
				})
			}
			require.NoError(t, h.logStore.InsertMany(t.Context(), entries))

			get := func(query string) (ids []string, next string) {
				req := httptest.NewRequest(http.MethodGet, list.path+"?"+query, nil)
				resp := h.do(h.withAPIKey(req))
				require.Equal(t, http.StatusOK, resp.Code, "body: %s", resp.Body.String())

				var result struct {
					Models []struct {
						ID string `json:"id"`
					} `json:"models"`
					Pagination apirouter.SeekPagination `json:"pagination"`
				}
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
				ids = []string{}
				for _, model := range result.Models {
					ids = append(ids, model.ID)
				}
				if result.Pagination.Next != nil {
					next = *result.Pagination.Next
				}
				return ids, next
			}
			oldest, middle, newest := list.ids[0], list.ids[1], list.ids[2]

			// The cursor points past the middle record of the newest-first list.
			page, next := get("dir=desc&limit=2")
			require.Equal(t, []string{newest, middle}, page)
			require.NotEmpty(t, next)

			t.Run("same query continues the list", func(t *testing.T) {
				page, _ := get("dir=desc&limit=2&next=" + next)
				assert.Equal(t, []string{oldest}, page)
			})

			t.Run("another sort order lists from the position in that order", func(t *testing.T) {
				page, _ := get("dir=asc&limit=2&next=" + next)
				assert.Equal(t, []string{newest}, page)
			})

			t.Run("another filter lists from the position with that filter", func(t *testing.T) {
				page, _ := get("dir=desc&limit=2&topic=user.updated&next=" + next)
				assert.Empty(t, page)
			})
		})
	}
}

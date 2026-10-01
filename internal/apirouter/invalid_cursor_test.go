package apirouter_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

// Every list answers a cursor it cannot read with the same 400 and wording.
func TestAPI_InvalidCursorReturns400(t *testing.T) {
	const (
		invalidCursor   = "invalid cursor"
		versionMismatch = "invalid cursor: cursor version mismatch: expected version 01"
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
				cases = append(cases, cursorCase{"position that is not a timestamp", cursor.Encode("tnt", 1, "yesterday"), "invalid cursor: invalid timestamp"})
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

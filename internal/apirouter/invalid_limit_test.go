package apirouter_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var limitLists = []struct {
	name         string
	path         string
	defaultLimit int
	maxLimit     int
}{
	{"tenants", "/api/v1/tenants", 20, 100},
	{"events", "/api/v1/events", 100, 1000},
	{"attempts", "/api/v1/attempts", 100, 1000},
	{"destination attempts", "/api/v1/tenants/t1/destinations/d1/attempts", 100, 1000},
}

func newLimitTest(t *testing.T) *apiTest {
	t.Helper()
	h := newAPITest(t)
	require.NoError(t, h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
	require.NoError(t, h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1"))))
	return h
}

// Every list answers a limit it cannot use with the same 400 and wording,
// instead of falling back to the default or the maximum.
func TestAPI_InvalidLimitReturns400(t *testing.T) {
	const notAnInteger = "invalid limit: must be an integer"

	for _, list := range limitLists {
		t.Run(list.name, func(t *testing.T) {
			outOfRange := fmt.Sprintf("invalid limit: must be between 1 and %d", list.maxLimit)

			cases := []struct {
				name    string
				limit   string
				message string
			}{
				{"a word", "ten", notAnInteger},
				{"a decimal", "1.5", notAnInteger},
				{"an exponent", "1e2", notAnInteger},
				{"a number with a unit", "10px", notAnInteger},
				{"a space", " ", notAnInteger},
				{"larger than an integer", "99999999999999999999", notAnInteger},
				{"zero", "0", outOfRange},
				{"negative", "-1", outOfRange},
				{"one over the maximum", strconv.Itoa(list.maxLimit + 1), outOfRange},
				{"far over the maximum", "1000000", outOfRange},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					h := newLimitTest(t)

					query := url.Values{"limit": {tc.limit}}
					req := httptest.NewRequest(http.MethodGet, list.path+"?"+query.Encode(), nil)
					resp := h.do(h.withAPIKey(req))

					testutil.RequireErrorResponse(t, resp, http.StatusBadRequest, tc.message)
				})
			}
		})
	}
}

func TestAPI_ValidLimitIsUsed(t *testing.T) {
	for _, list := range limitLists {
		t.Run(list.name, func(t *testing.T) {
			cases := []struct {
				name  string
				query string
				want  int
			}{
				{"no limit uses the default", "", list.defaultLimit},
				{"empty limit uses the default", "limit=", list.defaultLimit},
				{"minimum", "limit=1", 1},
				{"maximum", "limit=" + strconv.Itoa(list.maxLimit), list.maxLimit},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					h := newLimitTest(t)

					req := httptest.NewRequest(http.MethodGet, list.path+"?"+tc.query, nil)
					resp := h.do(h.withAPIKey(req))

					require.Equal(t, http.StatusOK, resp.Code, "body: %s", resp.Body.String())
					var result struct {
						Pagination struct {
							Limit int `json:"limit"`
						} `json:"pagination"`
					}
					require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
					assert.Equal(t, tc.want, result.Pagination.Limit)
				})
			}
		})
	}
}

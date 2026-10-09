package apirouter_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mcpRoutes are the six MCP endpoints, with a request body where they take
// one.
var mcpRoutes = []struct {
	method, path string
	body         any
}{
	{http.MethodGet, "/tenants/t1/mcp/events", nil},
	{http.MethodPut, "/tenants/t1/mcp/subscriptions", subscribeBody("p1", "order.created")},
	{http.MethodPost, "/tenants/t1/mcp/subscriptions/unsubscribe", subscribeBody("p1", "order.created")},
	{http.MethodGet, "/tenants/t1/mcp/subscriptions", nil},
	{http.MethodDelete, "/tenants/t1/mcp/subscriptions/sub_x", nil},
	{http.MethodDelete, "/tenants/t1/mcp/subscriptions?principal=p1", nil},
}

func TestMCP_Routes(t *testing.T) {
	t.Run("without MCP deps the v2 routes answer 503", func(t *testing.T) {
		m := newMCPTest(t, withoutMCPDeps())
		for _, route := range mcpRoutes {
			resp := m.do(m.withAPIKey(m.jsonReq(route.method, "/api/v2"+route.path, route.body)))
			testutil.RequireErrorResponse(t, resp, http.StatusServiceUnavailable, "mcp events is not configured")
		}
	})

	t.Run("v1 doesn't serve them", func(t *testing.T) {
		m := newMCPTest(t)
		for _, route := range mcpRoutes {
			resp := m.do(m.withAPIKey(m.jsonReq(route.method, "/api/v1"+route.path, route.body)))
			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "not found")
		}
	})

	t.Run("auth", func(t *testing.T) {
		m := newMCPTest(t)
		require.NoError(t, m.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2"))))
		jwtAllowed := map[string]bool{
			http.MethodGet + " /tenants/t1/mcp/subscriptions":          true,
			http.MethodDelete + " /tenants/t1/mcp/subscriptions/sub_x": true,
		}
		for _, route := range mcpRoutes {
			name := route.method + " " + strings.Split(route.path, "?")[0]
			t.Run(name, func(t *testing.T) {
				resp := m.do(m.jsonReq(route.method, "/api/v2"+route.path, route.body))
				testutil.RequireErrorResponse(t, resp, http.StatusUnauthorized, "unauthorized")

				resp = m.do(m.withJWT(m.jsonReq(route.method, "/api/v2"+route.path, route.body), "t2"))
				testutil.RequireErrorResponse(t, resp, http.StatusForbidden, "forbidden")

				resp = m.do(m.withJWT(m.jsonReq(route.method, "/api/v2"+route.path, route.body), "t1"))
				if jwtAllowed[name] {
					assert.NotEqual(t, http.StatusForbidden, resp.Code, resp.Body.String())
				} else {
					testutil.RequireErrorResponse(t, resp, http.StatusForbidden, "forbidden")
				}
			})
		}
	})

	t.Run("tenant-scoped operator routes need the tenant", func(t *testing.T) {
		m := newMCPTest(t)
		for _, path := range []string{
			"/tenants/missing/mcp/subscriptions",
			"/tenants/missing/mcp/subscriptions/sub_x",
			"/tenants/missing/mcp/subscriptions?principal=p1",
		} {
			method := http.MethodDelete
			if path == "/tenants/missing/mcp/subscriptions" {
				method = http.MethodGet
			}
			resp := m.do(m.withAPIKey(m.jsonReq(method, "/api/v2"+path, nil)))
			testutil.RequireErrorResponse(t, resp, http.StatusNotFound, "tenant not found")
		}
	})
}

func (m *mcpTest) listEvents(query string) (int, string) {
	m.t.Helper()
	resp := m.do(m.withAPIKey(m.jsonReq(http.MethodGet, "/api/v2/tenants/anyone/mcp/events"+query, nil)))
	return resp.Code, resp.Body.String()
}

// eventsJSON is the events/list result holding the named events.
func eventsJSON(t *testing.T, catalog *topicschema.Catalog, nextCursor string, names ...string) string {
	t.Helper()
	parts := make([]string, len(names))
	for i, name := range names {
		event, ok := catalog.MCPEvent(name)
		require.True(t, ok, name)
		parts[i] = string(event.JSON)
	}
	body := `{"events":[` + strings.Join(parts, ",") + `]`
	if nextCursor != "" {
		body += `,"nextCursor":"` + nextCursor + `"`
	}
	return body + "}"
}

func eventsCursor(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte("t:" + name))
}

func TestMCP_ListEvents(t *testing.T) {
	m := newMCPTest(t)

	t.Run("lists the MCP-enabled topics in TOPICS order, for any tenant", func(t *testing.T) {
		status, body := m.listEvents("")
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, eventsJSON(t, m.catalog, "", "order.created", "order.shipped"), body)

		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &parsed))
		assert.NotContains(t, parsed, "nextCursor", "nextCursor is omitted on the last page")
	})

	t.Run("topics narrows the list", func(t *testing.T) {
		cases := map[string][]string{
			"?topics=order.shipped":                       {"order.shipped"},
			"?topics=order.shipped,order.created":         {"order.created", "order.shipped"},
			"?topics=order.shipped&topics=order.created":  {"order.created", "order.shipped"},
			"?topics=order.shipped,unknown,user.created,": {"order.shipped"},
			"?topics=":             {},
			"?topics=&topics=":     {},
			"?topics=user.created": {},
			"?topics=" + strings.Repeat("x,", 100) + "order": {},
		}
		for query, want := range cases {
			status, body := m.listEvents(query)
			require.Equal(t, http.StatusOK, status, query)
			assert.Equal(t, eventsJSON(t, m.catalog, "", want...), body, query)
		}
	})

	t.Run("pages with limit and cursor", func(t *testing.T) {
		status, body := m.listEvents("?limit=1")
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, eventsJSON(t, m.catalog, eventsCursor("order.created"), "order.created"), body)

		status, body = m.listEvents("?limit=1&cursor=" + eventsCursor("order.created"))
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, eventsJSON(t, m.catalog, "", "order.shipped"), body)

		// The cursor is a position: it holds whatever topics the next
		// call allows.
		status, body = m.listEvents("?cursor=" + eventsCursor("order.created") + "&topics=order.created")
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, `{"events":[]}`, body)

		// No next page when the remaining entries aren't allowed.
		status, body = m.listEvents("?limit=1&topics=order.created")
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, eventsJSON(t, m.catalog, "", "order.created"), body)

		// Padded cursors are accepted too.
		status, _ = m.listEvents("?cursor=" + base64.URLEncoding.EncodeToString([]byte("t:order.created")))
		require.Equal(t, http.StatusOK, status)
	})

	t.Run("invalid limit or cursor is invalid_params", func(t *testing.T) {
		for _, query := range []string{"?limit=0", "?limit=101", "?limit=x", "?limit=-1", "?limit=1.5"} {
			resp := m.do(m.withAPIKey(m.jsonReq(http.MethodGet, "/api/v2/tenants/t1/mcp/events"+query, nil)))
			requireMCPError(t, resp, "invalid_params", -32602, map[string]any{"field": "limit", "reason": "invalid"})
		}
		for _, cursor := range []string{
			"!!!",
			base64.RawURLEncoding.EncodeToString([]byte("order.created")),
			eventsCursor("user.created"),
			eventsCursor("gone"),
			strings.Repeat("A", 5000),
		} {
			resp := m.do(m.withAPIKey(m.jsonReq(http.MethodGet, "/api/v2/tenants/t1/mcp/events?cursor="+cursor, nil)))
			requireMCPError(t, resp, "invalid_params", -32602, map[string]any{"field": "cursor", "reason": "invalid"})
		}
	})

	t.Run("errors use the configured code profile", func(t *testing.T) {
		m := newMCPTest(t, withMCPProfile(mcpevents.CodeProfileSEP3415))
		resp := m.do(m.withAPIKey(m.jsonReq(http.MethodGet, "/api/v2/tenants/t1/mcp/events?limit=0", nil)))
		requireMCPError(t, resp, "invalid_params", -32602, map[string]any{"field": "limit", "reason": "invalid"})

		resp = m.subscribe(subscribeBody("p1", "nope"))
		requireMCPError(t, resp, "not_found", -32023, map[string]any{"kind": "event"})
	})

	t.Run("without MCP topics the list is empty", func(t *testing.T) {
		m := newMCPTest(t, withMCPCatalog(topicschema.EmptyCatalog(mcpTopics)))
		status, body := m.listEvents("")
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, `{"events":[]}`, body)
	})
}

// A page stops before 1 MiB, and still holds an entry larger than that.
func TestMCP_ListEvents_ByteBudget(t *testing.T) {
	// MCP payload schemas are capped at 64 KiB: 20 topics of about 60 KiB
	// take two pages.
	var topics []string
	defs := topicschema.Definitions{}
	for i := range 20 {
		name := fmt.Sprintf("big.%02d", i)
		topics = append(topics, name)
		schema := fmt.Sprintf(`{"type":"object","description":%q,"properties":{"n":{"type":"number"}}}`, strings.Repeat("d", 60<<10))
		defs[name] = topicschema.Definition{PayloadSchema: json.RawMessage(schema), MCP: topicschema.MCPSettings{Enabled: true}}
	}
	catalog, err := topicschema.NewCatalog(topics, defs)
	require.NoError(t, err)
	m := newMCPTest(t, withMCPCatalog(catalog), withMCPAPIOptions(withTopics(topics)))

	var seen []string
	query := ""
	for range 5 {
		status, body := m.listEvents(query)
		require.Equal(t, http.StatusOK, status)
		assert.LessOrEqual(t, len(body), 1<<20)

		var page struct {
			Events []struct {
				Name string `json:"name"`
			} `json:"events"`
			NextCursor string `json:"nextCursor"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &page))
		require.NotEmpty(t, page.Events)
		for _, e := range page.Events {
			seen = append(seen, e.Name)
		}
		if page.NextCursor == "" {
			break
		}
		assert.Less(t, len(page.Events), 20, "the first page is cut by size, not by limit")
		query = "?cursor=" + page.NextCursor
	}
	assert.Equal(t, topics, seen)
}

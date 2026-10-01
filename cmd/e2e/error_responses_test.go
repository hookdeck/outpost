package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every error the API returns is the JSON envelope {status, message}. The
// request helpers fail on any API response that is not JSON with a body; the
// tests in the error_responses_*_test.go files assert the status and message
// of each error a route can return against a running server.

type errorResponse struct {
	Status  int             `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// rawBody is a request body sent as it is, without JSON encoding.
type rawBody string

// errorCase is one request and the error it must return.
type errorCase struct {
	name    string
	method  string
	path    string // under /api/v1, unless url is set
	url     string // full URL, for paths outside /api/v1
	auth    string // Authorization header; empty sends none
	body    any    // JSON-encoded, or a rawBody
	status  int
	message string
	detail  string // optional: must appear in the envelope's data
}

func (s *basicSuite) adminAuth() string {
	return "Bearer " + s.config.APIKey
}

// tenantAuth returns the Authorization header of a JWT for the tenant.
func (s *basicSuite) tenantAuth(tenantID string) string {
	s.T().Helper()
	var token struct {
		Token string `json:"token"`
	}
	status := s.doJSON(http.MethodGet, s.apiURL("/tenants/"+tenantID+"/token"), nil, &token)
	s.Require().Equal(http.StatusOK, status)
	s.Require().NotEmpty(token.Token)
	return "Bearer " + token.Token
}

func (s *basicSuite) deleteTenant(tenantID string) {
	s.T().Helper()
	status := s.doJSON(http.MethodDelete, s.apiURL("/tenants/"+tenantID), nil, nil)
	s.Require().Equal(http.StatusOK, status, "failed to delete tenant %s", tenantID)
}

func (s *basicSuite) deleteDestination(tenantID, destID string) {
	s.T().Helper()
	status := s.doJSON(http.MethodDelete, s.apiURL("/tenants/"+tenantID+"/destinations/"+destID), nil, nil)
	s.Require().Equal(http.StatusOK, status, "failed to delete destination %s", destID)
}

// requireError sends the request and asserts the error envelope.
func (s *basicSuite) requireError(c errorCase) {
	s.T().Helper()

	url := c.url
	if url == "" {
		url = s.apiURL(c.path)
	}

	var resp errorResponse
	var status int
	if raw, ok := c.body.(rawBody); ok {
		status = s.doRawWithAuth(c.method, url, c.auth, []byte(raw), &resp)
	} else {
		status = s.doJSONWithAuth(c.method, url, c.auth, c.body, &resp)
	}

	s.Require().Equal(c.status, status, "message: %q data: %s", resp.Message, resp.Data)
	s.Equal(c.status, resp.Status)
	s.Equal(c.message, resp.Message)
	if c.detail != "" {
		s.Contains(string(resp.Data), c.detail)
	}
}

func (s *basicSuite) runErrorCases(cases []errorCase) {
	for _, c := range cases {
		s.Run(c.name, func() {
			s.requireError(c)
		})
	}
}

// apiRoute is one route of the API with its auth requirements.
type apiRoute struct {
	method        string
	path          string
	adminOnly     bool // rejects tenant JWTs
	requireTenant bool // 404 when the tenant in the path does not exist
}

func (r apiRoute) name() string {
	return r.method + " " + r.path
}

func (r apiRoute) tenantScoped() bool {
	return strings.Contains(r.path, ":tenant_id")
}

// fill replaces the path parameters. Parameters other than the tenant and
// the destination get IDs that do not exist.
func (r apiRoute) fill(tenantID, destinationID string) string {
	return strings.NewReplacer(
		":tenant_id", tenantID,
		":destination_id", destinationID,
		":attempt_id", "att_missing",
		":event_id", "evt_missing",
		":type", "webhook",
	).Replace(r.path)
}

// apiRoutes lists every route of the API.
// TestErrorResponses_RouteTableMatchesRouter fails when it differs from the router.
var apiRoutes = []apiRoute{
	{method: http.MethodGet, path: "/destination-types"},
	{method: http.MethodGet, path: "/destination-types/:type"},
	{method: http.MethodGet, path: "/topics"},

	{method: http.MethodPost, path: "/publish", adminOnly: true},
	{method: http.MethodPost, path: "/retry"},

	{method: http.MethodGet, path: "/tenants"},
	{method: http.MethodPut, path: "/tenants/:tenant_id"},
	{method: http.MethodGet, path: "/tenants/:tenant_id", requireTenant: true},
	{method: http.MethodDelete, path: "/tenants/:tenant_id", requireTenant: true},
	{method: http.MethodGet, path: "/tenants/:tenant_id/token", adminOnly: true, requireTenant: true},
	{method: http.MethodGet, path: "/tenants/:tenant_id/portal", adminOnly: true, requireTenant: true},

	{method: http.MethodGet, path: "/tenants/:tenant_id/destinations", requireTenant: true},
	{method: http.MethodPost, path: "/tenants/:tenant_id/destinations", requireTenant: true},
	{method: http.MethodGet, path: "/tenants/:tenant_id/destinations/:destination_id", requireTenant: true},
	{method: http.MethodPatch, path: "/tenants/:tenant_id/destinations/:destination_id", requireTenant: true},
	{method: http.MethodDelete, path: "/tenants/:tenant_id/destinations/:destination_id", requireTenant: true},
	{method: http.MethodPut, path: "/tenants/:tenant_id/destinations/:destination_id/enable", requireTenant: true},
	{method: http.MethodPut, path: "/tenants/:tenant_id/destinations/:destination_id/disable", requireTenant: true},
	{method: http.MethodGet, path: "/tenants/:tenant_id/destinations/:destination_id/attempts", requireTenant: true},
	{method: http.MethodGet, path: "/tenants/:tenant_id/destinations/:destination_id/attempts/:attempt_id", requireTenant: true},

	{method: http.MethodGet, path: "/events"},
	{method: http.MethodGet, path: "/events/:event_id"},

	{method: http.MethodGet, path: "/attempts"},
	{method: http.MethodGet, path: "/attempts/:attempt_id"},

	{method: http.MethodGet, path: "/metrics/events"},
	{method: http.MethodGet, path: "/metrics/attempts"},
}

type noopDeliveryPublisher struct{}

func (noopDeliveryPublisher) Publish(context.Context, models.DeliveryTask) error { return nil }

type noopEventHandler struct{}

func (noopEventHandler) Handle(context.Context, *models.Event) (*publishmq.HandleResult, error) {
	return &publishmq.HandleResult{}, nil
}

// The auth tests below go over apiRoutes, so the table has to list every route
// the router registers. Needs no infrastructure.
func TestErrorResponses_RouteTableMatchesRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger, err := logging.NewLogger(logging.WithLogLevel("fatal"))
	require.NoError(t, err)

	handler := apirouter.NewRouter(
		apirouter.RouterConfig{
			ServiceName: "test",
			APIKey:      "apikey",
			JWTSecret:   "jwtsecret",
			Registry:    destregistry.NewRegistry(&destregistry.Config{}, logger),
			GinMode:     gin.TestMode,
		},
		apirouter.RouterDeps{
			TenantStore:       tenantstore.NewMemTenantStore(),
			LogStore:          logstore.NewMemLogStore(),
			Logger:            logger,
			DeliveryPublisher: noopDeliveryPublisher{},
			EventHandler:      noopEventHandler{},
			Telemetry:         &telemetry.NoopTelemetry{},
		},
	)
	engine, ok := handler.(*gin.Engine)
	require.True(t, ok, "the API router is no longer a *gin.Engine; list its routes another way")

	var registered []string
	for _, route := range engine.Routes() {
		if path, ok := strings.CutPrefix(route.Path, "/api/v1"); ok {
			registered = append(registered, route.Method+" "+path)
		}
	}

	var listed []string
	for _, route := range apiRoutes {
		listed = append(listed, route.name())
	}

	assert.ElementsMatch(t, registered, listed)
}

func (s *basicSuite) TestErrorResponses_Unauthorized() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*")

	s.Run("no auth header", func() {
		for _, route := range apiRoutes {
			s.Run(route.name(), func() {
				s.requireError(errorCase{
					method:  route.method,
					path:    route.fill(tenant.ID, dest.ID),
					status:  http.StatusUnauthorized,
					message: "unauthorized",
				})
			})
		}
	})

	// Routes of each auth kind: open to tenants, admin-only, tenant-scoped.
	representative := []apiRoute{
		{method: http.MethodGet, path: "/topics"},
		{method: http.MethodGet, path: "/events"},
		{method: http.MethodPost, path: "/publish"},
		{method: http.MethodGet, path: "/tenants/:tenant_id"},
		{method: http.MethodGet, path: "/tenants/:tenant_id/token"},
		{method: http.MethodGet, path: "/tenants/:tenant_id/destinations/:destination_id"},
	}

	headers := []struct {
		name string
		auth string
	}{
		{"not a bearer token", "Basic abc"},
		{"invalid token", "Bearer not-a-token"},
	}
	for _, header := range headers {
		s.Run(header.name, func() {
			for _, route := range representative {
				s.Run(route.name(), func() {
					s.requireError(errorCase{
						method:  route.method,
						path:    route.fill(tenant.ID, dest.ID),
						auth:    header.auth,
						status:  http.StatusUnauthorized,
						message: "unauthorized",
					})
				})
			}
		})
	}

	s.Run("jwt of a deleted tenant", func() {
		gone := s.createTenant()
		goneDest := s.createWebhookDestination(gone.ID, "*")
		jwt := s.tenantAuth(gone.ID)
		s.deleteTenant(gone.ID)

		stale := []apiRoute{
			{method: http.MethodGet, path: "/topics"},
			{method: http.MethodGet, path: "/events"},
			{method: http.MethodPost, path: "/retry"},
			{method: http.MethodGet, path: "/tenants/:tenant_id"},
			{method: http.MethodGet, path: "/tenants/:tenant_id/destinations"},
			{method: http.MethodGet, path: "/tenants/:tenant_id/destinations/:destination_id"},
		}
		for _, route := range stale {
			s.Run(route.name(), func() {
				s.requireError(errorCase{
					method:  route.method,
					path:    route.fill(gone.ID, goneDest.ID),
					auth:    jwt,
					status:  http.StatusUnauthorized,
					message: "unauthorized",
				})
			})
		}
	})
}

func (s *basicSuite) TestErrorResponses_Forbidden() {
	tenant := s.createTenant()
	other := s.createTenant()
	otherDest := s.createWebhookDestination(other.ID, "*")
	jwt := s.tenantAuth(tenant.ID)

	s.Run("jwt on an admin-only route", func() {
		for _, route := range apiRoutes {
			if !route.adminOnly {
				continue
			}
			s.Run(route.name(), func() {
				s.requireError(errorCase{
					method:  route.method,
					path:    route.fill(tenant.ID, "des_missing"),
					auth:    jwt,
					status:  http.StatusForbidden,
					message: "forbidden",
				})
			})
		}
	})

	s.Run("jwt on another tenant", func() {
		for _, route := range apiRoutes {
			if !route.tenantScoped() {
				continue
			}
			s.Run(route.name(), func() {
				s.requireError(errorCase{
					method:  route.method,
					path:    route.fill(other.ID, otherDest.ID),
					auth:    jwt,
					status:  http.StatusForbidden,
					message: "forbidden",
				})
			})
		}
	})

	metricsQuery := "?time[start]=2024-01-01T00:00:00Z&time[end]=2024-01-02T00:00:00Z&measures[0]=count"

	s.runErrorCases([]errorCase{
		{
			name:   "jwt sets created_at on a destination",
			method: http.MethodPost, path: "/tenants/" + tenant.ID + "/destinations", auth: jwt,
			body: map[string]any{
				"type":       "webhook",
				"topics":     []string{"*"},
				"config":     map[string]any{"url": "https://example.com/hook"},
				"created_at": "2024-01-01T00:00:00Z",
			},
			status: http.StatusForbidden, message: "created_at and updated_at can only be set with API key authentication",
		},
		{
			name:   "jwt lists events of another tenant",
			method: http.MethodGet, path: "/events?tenant_id=" + other.ID, auth: jwt,
			status: http.StatusForbidden, message: "tenant_id query parameter does not match authenticated tenant",
		},
		{
			name:   "jwt lists attempts of another tenant",
			method: http.MethodGet, path: "/attempts?tenant_id=" + other.ID, auth: jwt,
			status: http.StatusForbidden, message: "tenant_id query parameter does not match authenticated tenant",
		},
		{
			name:   "jwt groups event metrics by tenant",
			method: http.MethodGet, path: "/metrics/events" + metricsQuery + "&dimensions[0]=tenant_id", auth: jwt,
			status: http.StatusForbidden, message: "tenant_id dimension is not allowed for tenant-scoped requests",
		},
		{
			name:   "jwt groups attempt metrics by tenant",
			method: http.MethodGet, path: "/metrics/attempts" + metricsQuery + "&dimensions[0]=tenant_id", auth: jwt,
			status: http.StatusForbidden, message: "tenant_id dimension is not allowed for tenant-scoped requests",
		},
		{
			name:   "jwt filters event metrics by another tenant",
			method: http.MethodGet, path: "/metrics/events" + metricsQuery + "&filters[tenant_id]=" + other.ID, auth: jwt,
			status: http.StatusForbidden, message: "filters[tenant_id] does not match authenticated tenant",
		},
		{
			name:   "jwt filters attempt metrics by another tenant",
			method: http.MethodGet, path: "/metrics/attempts" + metricsQuery + "&filters[tenant_id]=" + other.ID, auth: jwt,
			status: http.StatusForbidden, message: "filters[tenant_id] does not match authenticated tenant",
		},
	})
}

func (s *basicSuite) TestErrorResponses_TenantNotFound() {
	deleted := s.createTenant()
	deletedDest := s.createWebhookDestination(deleted.ID, "*")
	s.deleteTenant(deleted.ID)

	tenants := []struct {
		name          string
		tenantID      string
		destinationID string
	}{
		{"missing tenant", idgen.String(), "des_missing"},
		{"deleted tenant", deleted.ID, deletedDest.ID},
	}

	for _, tenant := range tenants {
		s.Run(tenant.name, func() {
			for _, route := range apiRoutes {
				if !route.requireTenant {
					continue
				}
				s.Run(route.name(), func() {
					s.requireError(errorCase{
						method:  route.method,
						path:    route.fill(tenant.tenantID, tenant.destinationID),
						auth:    s.adminAuth(),
						status:  http.StatusNotFound,
						message: "tenant not found",
					})
				})
			}
		})
	}
}

func (s *basicSuite) TestErrorResponses_UnknownRoute() {
	s.runErrorCases([]errorCase{
		{name: "unknown route", method: http.MethodGet, path: "/nope", auth: s.adminAuth(), status: http.StatusNotFound, message: "not found"},
		{name: "unknown route, no auth", method: http.MethodGet, path: "/nope", status: http.StatusNotFound, message: "not found"},
		{name: "unknown nested route", method: http.MethodGet, path: "/tenants/t1/nope", auth: s.adminAuth(), status: http.StatusNotFound, message: "not found"},
		{name: "wrong method", method: http.MethodDelete, path: "/publish", auth: s.adminAuth(), status: http.StatusNotFound, message: "not found"},
		{name: "wrong method on the health check", method: http.MethodPost, path: "/healthz", status: http.StatusNotFound, message: "not found"},
		{name: "unknown version", method: http.MethodGet, url: s.rootURL("/api/v2/tenants"), auth: s.adminAuth(), status: http.StatusNotFound, message: "not found"},
		{name: "api root", method: http.MethodGet, url: s.rootURL("/api"), auth: s.adminAuth(), status: http.StatusNotFound, message: "not found"},
		{name: "api root, not GET", method: http.MethodPost, url: s.rootURL("/api"), auth: s.adminAuth(), status: http.StatusNotFound, message: "not found"},
	})
}

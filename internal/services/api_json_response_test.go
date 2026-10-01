package services_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/hookdeck/outpost/internal/worker"
	"github.com/stretchr/testify/require"
)

const (
	testAPIKey    = "test-api-key"
	testJWTSecret = "test-jwt-secret"
)

type noopDeliveryPublisher struct{}

func (noopDeliveryPublisher) Publish(context.Context, models.DeliveryTask) error { return nil }

type eventHandler interface {
	Handle(context.Context, *models.Event) (*publishmq.HandleResult, error)
}

type noopEventHandler struct{}

func (noopEventHandler) Handle(context.Context, *models.Event) (*publishmq.HandleResult, error) {
	return &publishmq.HandleResult{}, nil
}

// conflictEventHandler reports every publish as an idempotency conflict.
type conflictEventHandler struct{}

func (conflictEventHandler) Handle(context.Context, *models.Event) (*publishmq.HandleResult, error) {
	return nil, idempotence.ErrConflict
}

// deleteNotFoundStore reports the tenant as gone when it is deleted.
type deleteNotFoundStore struct {
	tenantstore.TenantStore
}

func (s *deleteNotFoundStore) DeleteTenant(context.Context, string) error {
	return tenantstore.ErrTenantNotFound
}

// panicStore panics when the tenant "panic" is retrieved.
type panicStore struct {
	tenantstore.TenantStore
}

func (s *panicStore) RetrieveTenant(ctx context.Context, tenantID string) (*models.Tenant, error) {
	if tenantID == "panic" {
		panic("test panic")
	}
	return s.TenantStore.RetrieveTenant(ctx, tenantID)
}

// newBaseRouter mounts the API handler on the base router the same way the
// API service does.
func newBaseRouter(t *testing.T, store tenantstore.TenantStore, handler eventHandler) http.Handler {
	t.Helper()

	logger, err := logging.NewLogger(logging.WithLogLevel("error"))
	require.NoError(t, err)

	apiHandler := apirouter.NewRouter(
		apirouter.RouterConfig{
			ServiceName: "test",
			APIKey:      testAPIKey,
			JWTSecret:   testJWTSecret,
			Topics:      testutil.TestTopics,
			Registry:    destregistry.NewRegistry(&destregistry.Config{}, logger),
			GinMode:     gin.TestMode,
		},
		apirouter.RouterDeps{
			TenantStore:       store,
			LogStore:          logstore.NewMemLogStore(),
			Logger:            logger,
			DeliveryPublisher: noopDeliveryPublisher{},
			EventHandler:      handler,
			Telemetry:         &telemetry.NoopTelemetry{},
		},
	)

	router := services.NewBaseRouter(worker.NewWorkerSupervisor(logger), gin.TestMode, false)
	services.MountAPI(router, apiHandler)
	return router
}

// serve runs the request through the router. It fails the test when the
// response is not JSON with a body: every API response must be.
func serve(t *testing.T, router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	testutil.RequireJSONResponse(t, req.Method+" "+req.URL.Path, rec.Code, rec.Header(), rec.Body.Bytes())
	return rec
}

func jwtFor(t *testing.T, tenantID string) string {
	t.Helper()
	token, err := apirouter.JWT.New(testJWTSecret, apirouter.JWTClaims{TenantID: tenantID})
	require.NoError(t, err)
	return "Bearer " + token
}

// The API handler is served through the base router's NoRoute. A handler that
// sets 404 without writing a body gets gin's text/plain "404 page not found"
// from the base router, so the JSON error body has to be checked through it.
func TestBaseRouter_APINotFoundIsJSON(t *testing.T) {
	store := &deleteNotFoundStore{tenantstore.NewMemTenantStore()}
	tf, df := testutil.TenantFactory, testutil.DestinationFactory
	require.NoError(t, store.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
	require.NoError(t, store.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2"))))
	require.NoError(t, store.CreateDestination(t.Context(), df.Any(df.WithID("other"), df.WithTenantID("t2"))))
	require.NoError(t, store.CreateDestination(t.Context(), df.Any(df.WithID("deleted"), df.WithTenantID("t1"))))
	require.NoError(t, store.DeleteDestination(t.Context(), "t1", "deleted"))

	router := newBaseRouter(t, store, noopEventHandler{})

	tests := []struct {
		name    string
		method  string
		path    string
		body    string
		message string
	}{
		{"get missing destination", http.MethodGet, "/api/v1/tenants/t1/destinations/nope", "", "destination not found"},
		{"get deleted destination", http.MethodGet, "/api/v1/tenants/t1/destinations/deleted", "", "destination not found"},
		{"get destination of other tenant", http.MethodGet, "/api/v1/tenants/t1/destinations/other", "", "destination not found"},
		{"update missing destination", http.MethodPatch, "/api/v1/tenants/t1/destinations/nope", `{"topics":["user.created"]}`, "destination not found"},
		{"delete missing destination", http.MethodDelete, "/api/v1/tenants/t1/destinations/nope", "", "destination not found"},
		{"enable missing destination", http.MethodPut, "/api/v1/tenants/t1/destinations/nope/enable", "", "destination not found"},
		{"disable missing destination", http.MethodPut, "/api/v1/tenants/t1/destinations/nope/disable", "", "destination not found"},
		{"unknown destination type", http.MethodGet, "/api/v1/destination-types/nope", "", "destination type not found"},
		{"tenant gone at delete", http.MethodDelete, "/api/v1/tenants/t1", "", "tenant not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+testAPIKey)

			testutil.RequireErrorResponse(t, serve(t, router, req), http.StatusNotFound, tt.message)
		})
	}
}

// Every response the API serves is JSON with a body, whatever the status.
func TestBaseRouter_APIResponsesAreJSON(t *testing.T) {
	store := &panicStore{tenantstore.NewMemTenantStore()}
	tf := testutil.TenantFactory
	for _, id := range []string{"t1", "t2", "gone"} {
		require.NoError(t, store.UpsertTenant(t.Context(), tf.Any(tf.WithID(id))))
	}
	require.NoError(t, store.DeleteTenant(t.Context(), "gone"))

	router := newBaseRouter(t, store, conflictEventHandler{})

	const publishBody = `{"tenant_id":"t1","topic":"user.created","data":{"key":"value"}}`
	admin := "Bearer " + testAPIKey

	errorTests := []struct {
		name    string
		method  string
		path    string
		auth    string
		body    string
		status  int
		message string
	}{
		{"401 no auth header", http.MethodGet, "/api/v1/tenants/t1", "", "", http.StatusUnauthorized, "unauthorized"},
		{"401 malformed auth header", http.MethodGet, "/api/v1/tenants/t1", "Basic abc", "", http.StatusUnauthorized, "unauthorized"},
		{"401 invalid token", http.MethodGet, "/api/v1/tenants/t1", "Bearer not-a-token", "", http.StatusUnauthorized, "unauthorized"},
		{"401 jwt without tenant", http.MethodGet, "/api/v1/topics", jwtFor(t, ""), "", http.StatusUnauthorized, "unauthorized"},
		{"401 jwt of deleted tenant", http.MethodGet, "/api/v1/tenants/gone", jwtFor(t, "gone"), "", http.StatusUnauthorized, "unauthorized"},
		{"401 jwt of missing tenant", http.MethodGet, "/api/v1/tenants/nope", jwtFor(t, "nope"), "", http.StatusUnauthorized, "unauthorized"},
		{"403 jwt on admin-only route", http.MethodPost, "/api/v1/publish", jwtFor(t, "t1"), publishBody, http.StatusForbidden, "forbidden"},
		{"403 jwt on another tenant", http.MethodGet, "/api/v1/tenants/t2", jwtFor(t, "t1"), "", http.StatusForbidden, "forbidden"},
		{"409 publish conflict", http.MethodPost, "/api/v1/publish", admin, publishBody, http.StatusConflict, "event is already being processed"},
		{"404 unknown route", http.MethodGet, "/api/v1/nope", admin, "", http.StatusNotFound, "not found"},
		{"404 unknown route, no auth", http.MethodGet, "/api/v1/nope", "", "", http.StatusNotFound, "not found"},
		{"404 wrong method", http.MethodDelete, "/api/v1/publish", admin, "", http.StatusNotFound, "not found"},
		{"404 wrong method on health check", http.MethodPost, "/api/v1/healthz", "", "", http.StatusNotFound, "not found"},
		{"404 unknown version", http.MethodGet, "/api/v2/tenants", admin, "", http.StatusNotFound, "not found"},
		{"404 api root", http.MethodGet, "/api", admin, "", http.StatusNotFound, "not found"},
		{"404 api root, not GET", http.MethodPost, "/api", admin, "", http.StatusNotFound, "not found"},
		{"404 missing tenant", http.MethodGet, "/api/v1/tenants/nope", admin, "", http.StatusNotFound, "tenant not found"},
		{"422 invalid body", http.MethodPost, "/api/v1/publish", admin, `{`, http.StatusUnprocessableEntity, "invalid JSON"},
		{"500 panic in handler chain", http.MethodGet, "/api/v1/tenants/panic", admin, "", http.StatusInternalServerError, "internal server error"},
	}

	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}

			testutil.RequireErrorResponse(t, serve(t, router, req), tt.status, tt.message)
		})
	}

	successTests := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"200 health check", http.MethodGet, "/api/v1/healthz", http.StatusOK},
		{"200 topics", http.MethodGet, "/api/v1/topics", http.StatusOK},
		{"200 tenant", http.MethodGet, "/api/v1/tenants/t1", http.StatusOK},
		{"201 new tenant", http.MethodPut, "/api/v1/tenants/t3", http.StatusCreated},
		{"200 delete tenant", http.MethodDelete, "/api/v1/tenants/t3", http.StatusOK},
	}

	for _, tt := range successTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Authorization", admin)

			require.Equal(t, tt.status, serve(t, router, req).Code)
		})
	}
}

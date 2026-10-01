package services_test

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/hookdeck/outpost/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type noopDeliveryPublisher struct{}

func (noopDeliveryPublisher) Publish(context.Context, models.DeliveryTask) error { return nil }

type noopEventHandler struct{}

func (noopEventHandler) Handle(context.Context, *models.Event) (*publishmq.HandleResult, error) {
	return &publishmq.HandleResult{}, nil
}

// deleteNotFoundStore reports the tenant as gone when it is deleted.
type deleteNotFoundStore struct {
	tenantstore.TenantStore
}

func (s *deleteNotFoundStore) DeleteTenant(context.Context, string) error {
	return tenantstore.ErrTenantNotFound
}

// The API handler is served through the base router's NoRoute. A handler that
// sets 404 without writing a body gets gin's text/plain "404 page not found"
// from the base router, so the JSON error body has to be checked through it.
func TestBaseRouter_APINotFoundIsJSON(t *testing.T) {
	const apiKey = "test-api-key"

	logger, err := logging.NewLogger(logging.WithLogLevel("error"))
	require.NoError(t, err)

	store := &deleteNotFoundStore{tenantstore.NewMemTenantStore()}
	tf, df := testutil.TenantFactory, testutil.DestinationFactory
	require.NoError(t, store.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
	require.NoError(t, store.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2"))))
	require.NoError(t, store.CreateDestination(t.Context(), df.Any(df.WithID("other"), df.WithTenantID("t2"))))
	require.NoError(t, store.CreateDestination(t.Context(), df.Any(df.WithID("deleted"), df.WithTenantID("t1"))))
	require.NoError(t, store.DeleteDestination(t.Context(), "t1", "deleted"))

	apiHandler := apirouter.NewRouter(
		apirouter.RouterConfig{
			ServiceName: "test",
			APIKey:      apiKey,
			Topics:      testutil.TestTopics,
			Registry:    destregistry.NewRegistry(&destregistry.Config{}, logger),
			GinMode:     gin.TestMode,
		},
		apirouter.RouterDeps{
			TenantStore:       store,
			LogStore:          logstore.NewMemLogStore(),
			Logger:            logger,
			DeliveryPublisher: noopDeliveryPublisher{},
			EventHandler:      noopEventHandler{},
			Telemetry:         &telemetry.NoopTelemetry{},
		},
	)

	// Same mount as the API service.
	router := services.NewBaseRouter(worker.NewWorkerSupervisor(logger), gin.TestMode, false)
	router.NoRoute(gin.WrapH(apiHandler))

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
			req.Header.Set("Authorization", "Bearer "+apiKey)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusNotFound, rec.Code)

			mediaType, _, _ := mime.ParseMediaType(rec.Header().Get("Content-Type"))
			assert.Equal(t, "application/json", mediaType)

			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body: %s", rec.Body.String())
			assert.Equal(t, float64(http.StatusNotFound), body["status"])
			assert.Equal(t, tt.message, body["message"])
		})
	}
}

package apirouter_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
)

var errStoreUnavailable = errors.New("store unavailable")

// failingTenantStore fails the operation named in fail and passes the rest
// through.
type failingTenantStore struct {
	tenantstore.TenantStore
	fail string
}

func (s *failingTenantStore) RetrieveTenant(ctx context.Context, tenantID string) (*models.Tenant, error) {
	if s.fail == "RetrieveTenant" {
		return nil, errStoreUnavailable
	}
	return s.TenantStore.RetrieveTenant(ctx, tenantID)
}

func (s *failingTenantStore) UpsertTenant(ctx context.Context, tenant models.Tenant) error {
	if s.fail == "UpsertTenant" {
		return errStoreUnavailable
	}
	return s.TenantStore.UpsertTenant(ctx, tenant)
}

func (s *failingTenantStore) DeleteTenant(ctx context.Context, tenantID string) error {
	if s.fail == "DeleteTenant" {
		return errStoreUnavailable
	}
	return s.TenantStore.DeleteTenant(ctx, tenantID)
}

func (s *failingTenantStore) ListTenant(ctx context.Context, req tenantstore.ListTenantRequest) (*tenantstore.TenantPaginatedResult, error) {
	if s.fail == "ListTenant" {
		return nil, errStoreUnavailable
	}
	return s.TenantStore.ListTenant(ctx, req)
}

func (s *failingTenantStore) ListDestination(ctx context.Context, req tenantstore.ListDestinationRequest) ([]models.Destination, error) {
	if s.fail == "ListDestination" {
		return nil, errStoreUnavailable
	}
	return s.TenantStore.ListDestination(ctx, req)
}

func (s *failingTenantStore) RetrieveDestination(ctx context.Context, tenantID, destinationID string) (*models.Destination, error) {
	if s.fail == "RetrieveDestination" {
		return nil, errStoreUnavailable
	}
	return s.TenantStore.RetrieveDestination(ctx, tenantID, destinationID)
}

func (s *failingTenantStore) CreateDestination(ctx context.Context, destination models.Destination) error {
	if s.fail == "CreateDestination" {
		return errStoreUnavailable
	}
	return s.TenantStore.CreateDestination(ctx, destination)
}

func (s *failingTenantStore) UpsertDestination(ctx context.Context, destination models.Destination) error {
	if s.fail == "UpsertDestination" {
		return errStoreUnavailable
	}
	return s.TenantStore.UpsertDestination(ctx, destination)
}

func (s *failingTenantStore) DeleteDestination(ctx context.Context, tenantID, destinationID string) error {
	if s.fail == "DeleteDestination" {
		return errStoreUnavailable
	}
	return s.TenantStore.DeleteDestination(ctx, tenantID, destinationID)
}

// failingLogStore fails the operation named in fail and passes the rest
// through.
type failingLogStore struct {
	logstore.LogStore
	fail string
}

func (s *failingLogStore) ListEvent(ctx context.Context, req logstore.ListEventRequest) (logstore.ListEventResponse, error) {
	if s.fail == "ListEvent" {
		return logstore.ListEventResponse{}, errStoreUnavailable
	}
	return s.LogStore.ListEvent(ctx, req)
}

func (s *failingLogStore) ListAttempt(ctx context.Context, req logstore.ListAttemptRequest) (logstore.ListAttemptResponse, error) {
	if s.fail == "ListAttempt" {
		return logstore.ListAttemptResponse{}, errStoreUnavailable
	}
	return s.LogStore.ListAttempt(ctx, req)
}

func (s *failingLogStore) RetrieveEvent(ctx context.Context, req logstore.RetrieveEventRequest) (*models.Event, error) {
	if s.fail == "RetrieveEvent" {
		return nil, errStoreUnavailable
	}
	return s.LogStore.RetrieveEvent(ctx, req)
}

func (s *failingLogStore) RetrieveAttempt(ctx context.Context, req logstore.RetrieveAttemptRequest) (*logstore.AttemptRecord, error) {
	if s.fail == "RetrieveAttempt" {
		return nil, errStoreUnavailable
	}
	return s.LogStore.RetrieveAttempt(ctx, req)
}

func (s *failingLogStore) QueryEventMetrics(ctx context.Context, req logstore.MetricsRequest) (*logstore.EventMetricsResponse, error) {
	if s.fail == "QueryEventMetrics" {
		return nil, errStoreUnavailable
	}
	return s.LogStore.QueryEventMetrics(ctx, req)
}

func (s *failingLogStore) QueryAttemptMetrics(ctx context.Context, req logstore.MetricsRequest) (*logstore.AttemptMetricsResponse, error) {
	if s.fail == "QueryAttemptMetrics" {
		return nil, errStoreUnavailable
	}
	return s.LogStore.QueryAttemptMetrics(ctx, req)
}

// A store failure on any route is a 500 with the JSON error envelope.
func TestAPI_StoreFailuresReturn500(t *testing.T) {
	metricsQS := "?time[start]=" + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) +
		"&time[end]=" + time.Now().UTC().Format(time.RFC3339) + "&measures[0]=count"
	retryBody := map[string]any{"event_id": "e1", "destination_id": "d1"}
	destinationBody := map[string]any{
		"type":   "webhook",
		"topics": []string{"user.created"},
		"config": map[string]string{"url": "https://example.com/hook"},
	}

	tests := []struct {
		name       string
		tenantFail string
		logFail    string
		method     string
		path       string
		body       any
		jwtTenant  string
	}{
		// auth_middleware.go
		{name: "auth: tenant lookup with api key", tenantFail: "RetrieveTenant", method: http.MethodGet, path: "/tenants/t1"},
		{name: "auth: tenant lookup with jwt", tenantFail: "RetrieveTenant", method: http.MethodGet, path: "/topics", jwtTenant: "t1"},

		// tenant_handlers.go
		{name: "upsert tenant: lookup", tenantFail: "RetrieveTenant", method: http.MethodPut, path: "/tenants/t1"},
		{name: "upsert tenant: update", tenantFail: "UpsertTenant", method: http.MethodPut, path: "/tenants/t1"},
		{name: "upsert tenant: create", tenantFail: "UpsertTenant", method: http.MethodPut, path: "/tenants/new"},
		{name: "list tenants", tenantFail: "ListTenant", method: http.MethodGet, path: "/tenants"},
		{name: "delete tenant", tenantFail: "DeleteTenant", method: http.MethodDelete, path: "/tenants/t1"},

		// destination_handlers.go
		{name: "list destinations", tenantFail: "ListDestination", method: http.MethodGet, path: "/tenants/t1/destinations"},
		{name: "create destination", tenantFail: "CreateDestination", method: http.MethodPost, path: "/tenants/t1/destinations", body: destinationBody},
		{name: "retrieve destination", tenantFail: "RetrieveDestination", method: http.MethodGet, path: "/tenants/t1/destinations/d1"},
		{name: "update destination: lookup", tenantFail: "RetrieveDestination", method: http.MethodPatch, path: "/tenants/t1/destinations/d1", body: map[string]any{"topics": []string{"user.created"}}},
		{name: "update destination: write", tenantFail: "UpsertDestination", method: http.MethodPatch, path: "/tenants/t1/destinations/d1", body: map[string]any{"topics": []string{"user.created"}}},
		{name: "delete destination", tenantFail: "DeleteDestination", method: http.MethodDelete, path: "/tenants/t1/destinations/d1"},
		{name: "disable destination", tenantFail: "UpsertDestination", method: http.MethodPut, path: "/tenants/t1/destinations/d1/disable"},
		{name: "enable destination", tenantFail: "UpsertDestination", method: http.MethodPut, path: "/tenants/t1/destinations/disabled/enable"},

		// log_handlers.go
		{name: "list events", logFail: "ListEvent", method: http.MethodGet, path: "/events"},
		{name: "retrieve event", logFail: "RetrieveEvent", method: http.MethodGet, path: "/events/e1"},
		{name: "list attempts", logFail: "ListAttempt", method: http.MethodGet, path: "/attempts"},
		{name: "list attempts: destinations", tenantFail: "ListDestination", method: http.MethodGet, path: "/attempts?include=destination"},
		{name: "retrieve attempt", logFail: "RetrieveAttempt", method: http.MethodGet, path: "/attempts/a1"},
		{name: "retrieve attempt: destination", tenantFail: "RetrieveDestination", method: http.MethodGet, path: "/attempts/a1?include=destination"},
		{name: "list destination attempts", logFail: "ListAttempt", method: http.MethodGet, path: "/tenants/t1/destinations/d1/attempts"},
		{name: "list destination attempts: destination lookup", tenantFail: "RetrieveDestination", method: http.MethodGet, path: "/tenants/t1/destinations/d1/attempts"},
		{name: "retrieve destination attempt", logFail: "RetrieveAttempt", method: http.MethodGet, path: "/tenants/t1/destinations/d1/attempts/a1"},
		{name: "retrieve destination attempt: destination lookup", tenantFail: "RetrieveDestination", method: http.MethodGet, path: "/tenants/t1/destinations/d1/attempts/a1"},

		// metrics_handlers.go
		{name: "event metrics", logFail: "QueryEventMetrics", method: http.MethodGet, path: "/metrics/events" + metricsQS},
		{name: "attempt metrics", logFail: "QueryAttemptMetrics", method: http.MethodGet, path: "/metrics/attempts" + metricsQS},

		// retry_handlers.go
		{name: "retry: attempt lookup", logFail: "ListAttempt", method: http.MethodPost, path: "/retry", body: retryBody},
		{name: "retry: destination lookup", tenantFail: "RetrieveDestination", method: http.MethodPost, path: "/retry", body: retryBody},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenantStore := &failingTenantStore{TenantStore: tenantstore.NewMemTenantStore()}
			logStore := &failingLogStore{LogStore: logstore.NewMemLogStore()}
			h := newAPITest(t, withTenantStore(tenantStore), withLogStore(logStore))

			require.NoError(t, tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1"))))
			require.NoError(t, tenantStore.UpsertDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}),
			)))
			require.NoError(t, tenantStore.UpsertDestination(t.Context(), df.Any(
				df.WithID("disabled"), df.WithTenantID("t1"), df.WithDisabledAt(time.Now()),
			)))
			e := ef.AnyPointer(ef.WithID("e1"), ef.WithTenantID("t1"), ef.WithTopic("user.created"))
			require.NoError(t, logStore.InsertMany(t.Context(), []*models.LogEntry{
				{Event: e, Attempt: attemptForEvent(e, af.WithID("a1"), af.WithDestinationID("d1"))},
			}))

			tenantStore.fail = tt.tenantFail
			logStore.fail = tt.logFail

			req := h.jsonReq(tt.method, "/api/v1"+tt.path, tt.body)
			if tt.jwtTenant != "" {
				req = h.withJWT(req, tt.jwtTenant)
			} else {
				req = h.withAPIKey(req)
			}

			testutil.RequireErrorResponse(t, h.do(req), http.StatusInternalServerError, "internal server error")
		})
	}
}

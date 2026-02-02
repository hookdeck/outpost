package apirouter_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/portal"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const (
	testAPIKey    = "test-api-key"
	testJWTSecret = "test-jwt-secret"
)

var (
	tf = testutil.TenantFactory
	df = testutil.DestinationFactory
	ef = testutil.EventFactory
	af = testutil.AttemptFactory
)

// ---------------------------------------------------------------------------
// apiTest harness
// ---------------------------------------------------------------------------

type apiTest struct {
	t            *testing.T
	router       http.Handler
	tenantStore  tenantstore.TenantStore
	logStore     logstore.LogStore
	deliveryPub  *mockDeliveryPublisher
	eventHandler *mockEventHandler
}

type apiTestOption func(*apiTestConfig)

type apiTestConfig struct {
	tenantStore  tenantstore.TenantStore
	destRegistry destregistry.Registry
}

func withTenantStore(ts tenantstore.TenantStore) apiTestOption {
	return func(cfg *apiTestConfig) {
		cfg.tenantStore = ts
	}
}

func withDestRegistry(r destregistry.Registry) apiTestOption {
	return func(cfg *apiTestConfig) {
		cfg.destRegistry = r
	}
}

func newAPITest(t *testing.T, opts ...apiTestOption) *apiTest {
	t.Helper()

	cfg := apiTestConfig{
		tenantStore: tenantstore.NewMemTenantStore(),
	}
	for _, o := range opts {
		o(&cfg)
	}

	logger := &logging.Logger{Logger: otelzap.New(zap.NewNop())}
	ts := cfg.tenantStore
	ls := logstore.NewMemLogStore()
	dp := &mockDeliveryPublisher{}
	eh := &mockEventHandler{}

	var registry destregistry.Registry = &stubRegistry{}
	if cfg.destRegistry != nil {
		registry = cfg.destRegistry
	}

	router := apirouter.NewRouter(
		apirouter.RouterConfig{
			ServiceName:  "test",
			APIKey:       testAPIKey,
			JWTSecret:    testJWTSecret,
			Topics:       testutil.TestTopics,
			Registry:     registry,
			PortalConfig: portal.PortalConfig{},
		},
		apirouter.RouterDeps{
			TenantStore:       ts,
			LogStore:          ls,
			Logger:            logger,
			DeliveryPublisher: dp,
			EventHandler:      eh,
			Telemetry:         &telemetry.NoopTelemetry{},
		},
	)

	return &apiTest{
		t:            t,
		router:       router,
		tenantStore:  ts,
		logStore:     ls,
		deliveryPub:  dp,
		eventHandler: eh,
	}
}

// do executes a request and returns the response recorder.
func (a *apiTest) do(req *http.Request) *httptest.ResponseRecorder {
	a.t.Helper()
	w := httptest.NewRecorder()
	a.router.ServeHTTP(w, req)
	return w
}

// jsonReq builds an *http.Request with a JSON body and Content-Type header.
// body may be nil for requests with no body.
func (a *apiTest) jsonReq(method, path string, body any) *http.Request {
	a.t.Helper()
	var reader io.Reader
	if body != nil {
		bs, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
		}
		reader = strings.NewReader(string(bs))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// withAPIKey adds the API key auth header to the request.
func (a *apiTest) withAPIKey(req *http.Request) *http.Request {
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	return req
}

// withJWT adds a JWT auth header for the given tenant.
func (a *apiTest) withJWT(req *http.Request, tenantID string) *http.Request {
	a.t.Helper()
	token, err := apirouter.JWT.New(testJWTSecret, apirouter.JWTClaims{TenantID: tenantID})
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// mockDeliveryPublisher records Publish calls.
type mockDeliveryPublisher struct {
	calls []models.DeliveryTask
	err   error
}

func (m *mockDeliveryPublisher) Publish(_ context.Context, task models.DeliveryTask) error {
	m.calls = append(m.calls, task)
	return m.err
}

// mockEventHandler records Handle calls with configurable return values.
type mockEventHandler struct {
	calls  []*models.Event
	result *publishmq.HandleResult
	err    error
}

func (m *mockEventHandler) Handle(_ context.Context, event *models.Event) (*publishmq.HandleResult, error) {
	m.calls = append(m.calls, event)
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	return &publishmq.HandleResult{EventID: event.ID}, nil
}

// stubRegistry is a minimal destregistry.Registry for test setup.
// Most methods are unused — only the metadata-related ones matter for sanitizer init.
type stubRegistry struct{}

func (r *stubRegistry) ValidateDestination(context.Context, *models.Destination) error {
	return nil
}
func (r *stubRegistry) PublishEvent(context.Context, *models.Destination, *models.Event) (*models.Attempt, error) {
	return nil, nil
}
func (r *stubRegistry) DisplayDestination(dest *models.Destination) (*destregistry.DestinationDisplay, error) {
	return &destregistry.DestinationDisplay{Destination: dest}, nil
}
func (r *stubRegistry) PreprocessDestination(*models.Destination, *models.Destination, *destregistry.PreprocessDestinationOpts) error {
	return nil
}
func (r *stubRegistry) RegisterProvider(string, destregistry.Provider) error { return nil }
func (r *stubRegistry) ResolveProvider(*models.Destination) (destregistry.Provider, error) {
	return nil, nil
}
func (r *stubRegistry) ResolvePublisher(context.Context, *models.Destination) (destregistry.Publisher, error) {
	return nil, nil
}
func (r *stubRegistry) MetadataLoader() metadata.MetadataLoader { return nil }
func (r *stubRegistry) RetrieveProviderMetadata(string) (*metadata.ProviderMetadata, error) {
	return nil, nil
}
func (r *stubRegistry) ListProviderMetadata() []*metadata.ProviderMetadata { return nil }

package e2e_test

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/suite"
)

// TestE2E_CompatSignature boots Outpost with the compat signature configured
// to reproduce Standard Webhooks, then checks each delivery from the receiver's
// side: the compat headers verify with the official Standard Webhooks SDK and
// the primary headers verify with the algorithm from the "Verify Webhook
// Signatures" guide. It is its own suite so the main suites stay unchanged.
func TestE2E_CompatSignature(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &compatSignatureSuite{})
}

type compatSignatureSuite struct {
	suite.Suite
	base     *basicSuite
	receiver *captureServer
}

func (s *compatSignatureSuite) SetupSuite() {
	s.receiver = newCaptureServer()

	s.base = &basicSuite{
		logStorageType: configs.LogStorageTypeClickHouse,
		redisConfig:    testinfra.NewDragonflyStackConfig(s.T()),
		configure: func(cfg *config.Config) {
			cfg.Destinations.Webhook.Compat = config.DestinationWebhookCompatConfig{
				SignatureHeaderName:      "webhook-signature",
				SignatureContentTemplate: "{{.EventID}}.{{.Timestamp.Unix}}.{{.Body}}",
				SignatureHeaderTemplate:  `v1,{{index .Signatures 0}}{{range slice .Signatures 1}} v1,{{.}}{{end}}`,
				SignatureEncoding:        "base64",
				SignatureSecretEncoding:  "base64",
				SignatureSecretPrefix:    "whsec_",
				Headers: config.HeaderTemplates{
					"webhook-id":        "{{.EventID}}",
					"webhook-timestamp": "{{.Timestamp.Unix}}",
				},
			}
		},
	}
	s.base.SetT(s.T())
	s.base.SetupSuite()
}

func (s *compatSignatureSuite) SetupTest() {
	s.base.SetT(s.T())
}

func (s *compatSignatureSuite) TearDownSuite() {
	s.receiver.Close()
	s.base.TearDownSuite()
}

func (s *compatSignatureSuite) TestStandardWebhooksSDKAndOutpostVerify() {
	tenant := s.base.createTenant()
	secret := newStandardWebhooksSecret()
	dest := s.createDestination(tenant.ID, secret)

	event := s.base.publish(tenant.ID, "user.created", map[string]any{"hello": "world"})
	req := s.receiver.wait(s.T(), dest)

	s.assertStandardWebhooks(secret, req)
	s.assertOutpostDefault(secret, req)

	s.Equal(event.ID, req.Header.Get("webhook-id"))
	s.Equal(event.ID, req.Header.Get("x-outpost-event-id"))
	primaryTS, err := time.Parse(time.RFC3339, req.Header.Get("x-outpost-timestamp"))
	s.Require().NoError(err)
	s.Equal(strconv.FormatInt(primaryTS.Unix(), 10), req.Header.Get("webhook-timestamp"))
}

func (s *compatSignatureSuite) TestSecretRotationSignsWithBothSecrets() {
	tenant := s.base.createTenant()
	oldSecret := newStandardWebhooksSecret()
	newSecret := newStandardWebhooksSecret()
	dest := s.createDestination(tenant.ID, oldSecret)

	s.patchDestination(tenant.ID, dest, map[string]any{
		"credentials": map[string]any{
			"secret":                     newSecret,
			"previous_secret":            oldSecret,
			"previous_secret_invalid_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		},
	})

	s.base.publish(tenant.ID, "user.created", map[string]any{"hello": "rotation"})
	req := s.receiver.wait(s.T(), dest)

	s.assertStandardWebhooks(newSecret, req)
	s.assertStandardWebhooks(oldSecret, req)
	s.assertOutpostDefault(newSecret, req)
	s.assertOutpostDefault(oldSecret, req)
}

func (s *compatSignatureSuite) TestUndecodableSecretSkipsCompat() {
	tenant := s.base.createTenant()
	secret := "plain-secret-not-base64!" // '-' and '!' are outside the base64 alphabet
	dest := s.createDestination(tenant.ID, secret)

	s.base.publish(tenant.ID, "user.created", map[string]any{"hello": "raw"})
	req := s.receiver.wait(s.T(), dest)

	s.assertNoCompatHeaders(req)
	s.assertOutpostDefault(secret, req)
}

// =============================================================================
// Verification, written the way a receiver would
// =============================================================================

// assertStandardWebhooks verifies the compat headers with the official
// Standard Webhooks Go SDK.
func (s *compatSignatureSuite) assertStandardWebhooks(secret string, req capturedRequest) {
	s.T().Helper()
	wh, err := standardwebhooks.NewWebhook(secret)
	s.Require().NoError(err)
	s.NoError(wh.Verify(req.Body, req.Header), "Standard Webhooks SDK should verify the compat signature")
}

// assertOutpostDefault verifies the primary headers following the "Verify
// Webhook Signatures" guide: hex(HMAC-SHA256(secret, body)), header
// "v0=<sig>[,<sig>]", any candidate matching is enough.
func (s *compatSignatureSuite) assertOutpostDefault(secret string, req capturedRequest) {
	s.T().Helper()
	header := req.Header.Get("x-outpost-signature")
	s.Require().True(strings.HasPrefix(header, "v0="), "primary signature header %q", header)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(req.Body)
	expected := hex.EncodeToString(mac.Sum(nil))

	for _, candidate := range strings.Split(strings.TrimPrefix(header, "v0="), ",") {
		if hmac.Equal([]byte(candidate), []byte(expected)) {
			return
		}
	}
	s.Failf("primary signature", "no candidate in %q matches secret", header)
}

func (s *compatSignatureSuite) assertNoCompatHeaders(req capturedRequest) {
	s.T().Helper()
	for _, name := range []string{"webhook-signature", "webhook-id", "webhook-timestamp"} {
		s.Empty(req.Header.Values(name), "header %s should not be sent", name)
	}
}

// =============================================================================
// Destination helpers
// =============================================================================

func newStandardWebhooksSecret() string {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return "whsec_" + base64.StdEncoding.EncodeToString(raw)
}

func (s *compatSignatureSuite) createDestination(tenantID, secret string) string {
	s.T().Helper()
	id := idgen.Destination()
	body := map[string]any{
		"id":          id,
		"type":        "webhook",
		"topics":      []string{"*"},
		"config":      map[string]any{"url": s.receiver.url(id)},
		"credentials": map[string]any{"secret": secret},
	}
	status := s.base.doJSON(http.MethodPost, s.base.apiURL("/tenants/"+tenantID+"/destinations"), body, nil)
	s.Require().Equal(http.StatusCreated, status, "failed to create destination %s", id)
	return id
}

func (s *compatSignatureSuite) patchDestination(tenantID, id string, body map[string]any) {
	s.T().Helper()
	status := s.base.doJSON(http.MethodPatch, s.base.apiURL("/tenants/"+tenantID+"/destinations/"+id), body, nil)
	s.Require().Equal(http.StatusOK, status, "failed to patch destination %s", id)
}

// =============================================================================
// Capture receiver: records every request per destination id
// =============================================================================

type capturedRequest struct {
	Header http.Header
	Body   []byte
}

type captureServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests map[string][]capturedRequest
}

func newCaptureServer() *captureServer {
	c := &captureServer{requests: map[string][]capturedRequest{}}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/")
		c.mu.Lock()
		c.requests[id] = append(c.requests[id], capturedRequest{Header: r.Header.Clone(), Body: body})
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	return c
}

func (c *captureServer) Close() { c.server.Close() }

func (c *captureServer) url(id string) string {
	return fmt.Sprintf("%s/%s", c.server.URL, id)
}

// wait returns the first request received for id, failing after the delivery
// timeout.
func (c *captureServer) wait(t *testing.T, id string) capturedRequest {
	t.Helper()
	deadline := time.Now().Add(mockServerPollTimeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		reqs := c.requests[id]
		c.mu.Unlock()
		if len(reqs) > 0 {
			return reqs[0]
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no delivery received for destination %s", id)
	return capturedRequest{}
}

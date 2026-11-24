package e2e_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/suite"
)

// TestE2E_StandardMode boots Outpost in "standard" webhook mode with a compat
// signature in Outpost's default format, then checks each delivery from the
// receiver's side: the primary headers verify with the official Standard
// Webhooks SDK and the compat header with the "Verify Webhook Signatures"
// guide's algorithm.
func TestE2E_StandardMode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &standardModeSuite{})
}

type standardModeSuite struct {
	suite.Suite
	base     *basicSuite
	receiver *captureServer
}

func (s *standardModeSuite) SetupSuite() {
	s.receiver = newCaptureServer()

	s.base = &basicSuite{
		logStorageType: configs.LogStorageTypeClickHouse,
		redisConfig:    testinfra.NewDragonflyStackConfig(s.T()),
		configure: func(cfg *config.Config) {
			cfg.Destinations.Webhook.Mode = "standard"
			// Header name alone: the rest defaults to Outpost's default scheme.
			cfg.Destinations.Webhook.Compat.SignatureHeaderName = "x-outpost-signature"
		},
	}
	s.base.SetT(s.T())
	s.base.SetupSuite()
}

func (s *standardModeSuite) SetupTest() {
	s.base.SetT(s.T())
}

func (s *standardModeSuite) TearDownSuite() {
	s.receiver.Close()
	s.base.TearDownSuite()
}

func (s *standardModeSuite) TestSDKVerifiesPrimaryAndCompatVerifiesDefault() {
	tenant := s.base.createTenant()
	secret := newStandardWebhooksSecret()
	dest := s.createDestination(tenant.ID, map[string]any{"secret": secret})

	event := s.base.publish(tenant.ID, "user.created", map[string]any{"hello": "standard"})
	req := s.receiver.wait(s.T(), dest)

	s.assertStandardWebhooks(secret, req)
	s.Equal(event.ID, req.Header.Get("webhook-id"))
	_, err := strconv.ParseInt(req.Header.Get("webhook-timestamp"), 10, 64)
	s.NoError(err, "webhook-timestamp %q should be unix seconds", req.Header.Get("webhook-timestamp"))
	s.Equal("user.created", req.Header.Get("webhook-topic"))

	// Compat with raw secret encoding: the stored string is the HMAC key.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(req.Body)
	s.Equal("v0="+hex.EncodeToString(mac.Sum(nil)), req.Header.Get("x-outpost-signature"))
}

func (s *standardModeSuite) TestGeneratedSecretIsStandardFormat() {
	tenant := s.base.createTenant()
	dest := s.createDestination(tenant.ID, nil)

	var got struct {
		Credentials map[string]string `json:"credentials"`
	}
	status := s.base.doJSON(http.MethodGet, s.base.apiURL("/tenants/"+tenant.ID+"/destinations/"+dest), nil, &got)
	s.Require().Equal(http.StatusOK, status)
	secret := got.Credentials["secret"]
	s.Require().True(strings.HasPrefix(secret, "whsec_"), "generated secret %q", secret)

	s.base.publish(tenant.ID, "user.created", map[string]any{"hello": "generated"})
	s.assertStandardWebhooks(secret, s.receiver.wait(s.T(), dest))
}

func (s *standardModeSuite) assertStandardWebhooks(secret string, req capturedRequest) {
	s.T().Helper()
	wh, err := standardwebhooks.NewWebhook(secret)
	s.Require().NoError(err)
	s.NoError(wh.Verify(req.Body, req.Header), "Standard Webhooks SDK should verify the primary signature")
}

func (s *standardModeSuite) createDestination(tenantID string, credentials map[string]any) string {
	s.T().Helper()
	id := idgen.Destination()
	body := map[string]any{
		"id":     id,
		"type":   "webhook",
		"topics": []string{"*"},
		"config": map[string]any{"url": s.receiver.url(id)},
	}
	if credentials != nil {
		body["credentials"] = credentials
	}
	status := s.base.doJSON(http.MethodPost, s.base.apiURL("/tenants/"+tenantID+"/destinations"), body, nil)
	s.Require().Equal(http.StatusCreated, status, "failed to create destination %s", id)
	return id
}

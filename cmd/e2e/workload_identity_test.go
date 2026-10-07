package e2e_test

import (
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/suite"
)

const (
	e2eWIFProvider   = "projects/123456/locations/global/workloadIdentityPools/outpost/providers/outpost"
	e2eServiceAcctJS = `{"type":"service_account","project_id":"my-project"}`
)

func gcpPubSubDestinationBody(config, credentials map[string]string) map[string]any {
	cfg := map[string]string{"project_id": "my-project", "topic": "my-topic"}
	for k, v := range config {
		cfg[k] = v
	}
	return map[string]any{"type": "gcp_pubsub", "topics": "*", "config": cfg, "credentials": credentials}
}

// Workload identity is off unless configured: no issuer routes, and the
// destination API rejects the auth method.
func (s *basicSuite) TestWorkloadIdentity_DisabledByDefault() {
	tenant := s.createTenant()
	admin := s.adminAuth()

	s.Run("tenant route", func() {
		s.requireError(errorCase{
			method: http.MethodGet, path: "/tenants/" + tenant.ID + "/workload-identity", auth: admin,
			status: http.StatusNotFound, message: "not found",
		})
	})

	s.Run("issuer routes", func() {
		for _, path := range []string{"/workload-identity/.well-known/openid-configuration", "/workload-identity/jwks.json"} {
			resp, err := s.httpClient.Get(s.rootURL(path))
			s.Require().NoError(err)
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			s.NotContains(string(body), "jwks_uri", path)
			s.NotContains(string(body), `"keys"`, path)
		}
	})

	s.Run("create with workload identity", func() {
		s.requireError(errorCase{
			method: http.MethodPost, path: "/tenants/" + tenant.ID + "/destinations", auth: admin,
			body: gcpPubSubDestinationBody(
				map[string]string{"auth_method": "workload_identity"},
				map[string]string{"workload_identity_provider": e2eWIFProvider, "service_account_json": e2eServiceAcctJS},
			),
			status: http.StatusUnprocessableEntity, message: validationError, detail: "config.auth_method failed enum validation",
		})
	})

	s.Run("create with a key", func() {
		var dest destinationResponse
		status := s.doJSON(http.MethodPost, s.apiURL("/tenants/"+tenant.ID+"/destinations"),
			gcpPubSubDestinationBody(nil, map[string]string{"service_account_json": e2eServiceAcctJS}), &dest)
		s.Require().Equal(http.StatusCreated, status)
		s.NotContains(dest.Config, "auth_method")
	})
}

// TestE2E_WorkloadIdentity boots Outpost as a workload identity issuer.
func TestE2E_WorkloadIdentity(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &workloadIdentitySuite{})
}

type workloadIdentitySuite struct {
	suite.Suite
	base   *basicSuite
	issuer string
}

func (s *workloadIdentitySuite) SetupSuite() {
	signingKey := testutil.SigningKeyPEM(s.T())

	s.base = &basicSuite{
		logStorageType: configs.LogStorageTypeClickHouse,
		redisConfig:    testinfra.NewDragonflyStackConfig(s.T()),
		configure: func(cfg *config.Config) {
			s.issuer = fmt.Sprintf("http://localhost:%d/workload-identity", cfg.APIPort)
			cfg.WorkloadIdentity.Issuer = s.issuer
			cfg.WorkloadIdentity.SigningKey = signingKey
		},
	}
	s.base.SetT(s.T())
	s.base.SetupSuite()
}

func (s *workloadIdentitySuite) SetupTest() {
	s.base.SetT(s.T())
}

func (s *workloadIdentitySuite) TearDownSuite() {
	s.base.TearDownSuite()
}

func (s *workloadIdentitySuite) TestIssuerEndpoints() {
	var discovery map[string]any
	s.Require().Equal(http.StatusOK, s.base.doJSONRaw(http.MethodGet, s.base.rootURL("/workload-identity/.well-known/openid-configuration"), nil, &discovery))
	s.Equal(s.issuer, discovery["issuer"])
	s.Equal(s.issuer+"/jwks.json", discovery["jwks_uri"])

	var jwks struct {
		Keys []map[string]string `json:"keys"`
	}
	s.Require().Equal(http.StatusOK, s.base.doJSONRaw(http.MethodGet, s.base.rootURL("/workload-identity/jwks.json"), nil, &jwks))
	s.Require().Len(jwks.Keys, 1)
	s.Equal("ES256", jwks.Keys[0]["alg"])
}

func (s *workloadIdentitySuite) TestTenantValues() {
	tenant := s.base.createTenant()
	var got map[string]string
	s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.base.apiURL("/tenants/"+tenant.ID+"/workload-identity"), nil, &got))
	s.Equal(map[string]string{"issuer": s.issuer, "subject": "tenant:" + tenant.ID}, got)
}

func (s *workloadIdentitySuite) TestDestinationTypeOffersAuthMethod() {
	var meta struct {
		ConfigFields []struct {
			Key     string `json:"key"`
			Options []struct {
				Value string `json:"value"`
			} `json:"options"`
		} `json:"config_fields"`
	}
	s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.base.apiURL("/destination-types/gcp_pubsub"), nil, &meta))
	var values []string
	for _, f := range meta.ConfigFields {
		if f.Key == "auth_method" {
			for _, o := range f.Options {
				values = append(values, o.Value)
			}
		}
	}
	s.Equal([]string{"service_account_key", "workload_identity"}, values)
}

func (s *workloadIdentitySuite) TestCreateDestination() {
	tenant := s.base.createTenant()

	var dest destinationResponse
	status := s.base.doJSON(http.MethodPost, s.base.apiURL("/tenants/"+tenant.ID+"/destinations"), gcpPubSubDestinationBody(
		map[string]string{"auth_method": "workload_identity"},
		map[string]string{"workload_identity_provider": e2eWIFProvider},
	), &dest)
	s.Require().Equal(http.StatusCreated, status)
	s.Equal("workload_identity", dest.Config["auth_method"])
	s.Equal(e2eWIFProvider, dest.Credentials["workload_identity_provider"])
	s.Contains(dest.Credentials, "service_account_json", "kept for clients that require the field")
	s.Equal("", dest.Credentials["service_account_json"])

	s.Run("with a key as well", func() {
		s.base.requireError(errorCase{
			method: http.MethodPost, path: "/tenants/" + tenant.ID + "/destinations", auth: s.base.adminAuth(),
			body: gcpPubSubDestinationBody(
				map[string]string{"auth_method": "workload_identity"},
				map[string]string{"workload_identity_provider": e2eWIFProvider, "service_account_json": e2eServiceAcctJS},
			),
			status: http.StatusUnprocessableEntity, message: validationError, detail: "credentials.service_account_json is forbidden",
		})
	})

	s.Run("switch to a key", func() {
		var updated destinationResponse
		status := s.base.doJSON(http.MethodPatch, s.base.apiURL("/tenants/"+tenant.ID+"/destinations/"+dest.ID), map[string]any{
			"config":      map[string]any{"auth_method": "service_account_key"},
			"credentials": map[string]any{"workload_identity_provider": nil, "service_account_json": e2eServiceAcctJS},
		}, &updated)
		s.Require().Equal(http.StatusOK, status)
		s.NotContains(updated.Credentials, "workload_identity_provider")
		s.NotEmpty(updated.Credentials["service_account_json"])
	})
}

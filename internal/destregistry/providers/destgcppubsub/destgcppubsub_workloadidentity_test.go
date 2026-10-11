package destgcppubsub_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destgcppubsub"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/hookdeck/outpost/internal/workloadidentity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	testProvider = "projects/123456/locations/global/workloadIdentityPools/outpost/providers/outpost"
	testAudience = "//iam.googleapis.com/" + testProvider
)

func newTestIssuer(t *testing.T) *workloadidentity.Issuer {
	t.Helper()
	issuer, err := workloadidentity.New(workloadidentity.Config{
		Issuer:     "https://outpost.example.com/workload-identity",
		SigningKey: testutil.SigningKeyPEM(t),
	})
	require.NoError(t, err)
	return issuer
}

func gcpDestination(tenantID string, config, credentials map[string]string) *models.Destination {
	cfg := map[string]string{"project_id": "my-project", "topic": "my-topic"}
	for k, v := range config {
		cfg[k] = v
	}
	d := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("gcp_pubsub"),
		testutil.DestinationFactory.WithTenantID(tenantID),
		testutil.DestinationFactory.WithConfig(cfg),
		testutil.DestinationFactory.WithCredentials(credentials),
	)
	return &d
}

// Without workload identity the destination type must look exactly as it did
// before the auth method existed.
func TestMetadata_WorkloadIdentityDisabledMatchesServiceAccountOnlySchema(t *testing.T) {
	t.Parallel()
	provider, err := destgcppubsub.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	var legacy struct {
		ConfigFields     []metadata.FieldSchema `json:"config_fields"`
		CredentialFields []metadata.FieldSchema `json:"credential_fields"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{
		"config_fields": [
			{"key": "project_id", "type": "text", "label": "Project ID", "description": "The GCP project ID", "required": true},
			{"key": "topic", "type": "text", "label": "Topic", "description": "The Pub/Sub topic", "required": true},
			{"key": "endpoint", "type": "text", "label": "Endpoint", "description": "Custom endpoint URL (e.g., localhost:8085 for emulator)", "required": false}
		],
		"credential_fields": [
			{"key": "service_account_json", "type": "text", "label": "Service Account JSON", "description": "Service account key JSON", "required": true, "sensitive": true}
		]
	}`), &legacy))

	want, err := json.Marshal(legacy)
	require.NoError(t, err)
	got, err := json.Marshal(struct {
		ConfigFields     []metadata.FieldSchema `json:"config_fields"`
		CredentialFields []metadata.FieldSchema `json:"credential_fields"`
	}{provider.Metadata().ConfigFields, provider.Metadata().CredentialFields})
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))

	assert.NotContains(t, provider.Metadata().Instructions, "Workload Identity")
}

func TestMetadata_WorkloadIdentityEnabled(t *testing.T) {
	t.Parallel()
	provider, err := destgcppubsub.New(testutil.Registry.MetadataLoader(), nil, destgcppubsub.WithWorkloadIdentity(newTestIssuer(t)))
	require.NoError(t, err)
	meta := provider.Metadata()

	auth, ok := meta.Field("auth_method")
	require.True(t, ok)
	assert.Equal(t, []metadata.FieldOption{
		{Label: "Service account key", Value: "service_account_key"},
		{Label: "Workload Identity Federation", Value: "workload_identity"},
	}, auth.Options)

	for key, method := range map[string]string{
		"service_account_json":       "service_account_key",
		"workload_identity_provider": "workload_identity",
		"service_account_email":      "workload_identity",
	} {
		f, ok := meta.Field(key)
		require.True(t, ok, key)
		assert.Equal(t, &metadata.FieldCondition{Key: "auth_method", Values: []string{method}}, f.VisibleWhen, key)
	}
	assert.Contains(t, meta.Instructions, "Workload Identity")
}

func TestValidate_AuthMethod(t *testing.T) {
	t.Parallel()
	serviceAccountJSON := `{"type":"service_account","project_id":"my-project"}`

	tests := []struct {
		name        string
		disabled    bool
		tenantID    string
		config      map[string]string
		credentials map[string]string
		want        []destregistry.ValidationErrorDetail
	}{
		{
			name:        "no auth method: service account key",
			credentials: map[string]string{"service_account_json": serviceAccountJSON},
		},
		{
			name:        "service account key",
			config:      map[string]string{"auth_method": "service_account_key"},
			credentials: map[string]string{"service_account_json": serviceAccountJSON},
		},
		{
			name:   "service account key missing",
			config: map[string]string{"auth_method": "service_account_key"},
			want:   []destregistry.ValidationErrorDetail{{Field: "credentials.service_account_json", Type: "required"}},
		},
		{
			name:        "workload identity",
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testProvider},
		},
		{
			name:        "workload identity with full resource name and impersonation",
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testAudience, "service_account_email": "publisher@my-project.iam.gserviceaccount.com"},
		},
		{
			name:   "workload identity provider missing",
			config: map[string]string{"auth_method": "workload_identity"},
			want:   []destregistry.ValidationErrorDetail{{Field: "credentials.workload_identity_provider", Type: "required"}},
		},
		{
			name:        "workload identity provider malformed",
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": "projects/my-project/locations/global/workloadIdentityPools/p/providers/q"},
			want:        []destregistry.ValidationErrorDetail{{Field: "credentials.workload_identity_provider", Type: "pattern"}},
		},
		{
			name:        "service account email malformed",
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testProvider, "service_account_email": "someone@example.com"},
			want:        []destregistry.ValidationErrorDetail{{Field: "credentials.service_account_email", Type: "pattern"}},
		},
		{
			name:        "both methods' credentials",
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testProvider, "service_account_json": serviceAccountJSON},
			want:        []destregistry.ValidationErrorDetail{{Field: "credentials.service_account_json", Type: "forbidden"}},
		},
		{
			name:        "workload identity settings without the auth method",
			credentials: map[string]string{"service_account_json": serviceAccountJSON, "workload_identity_provider": testProvider},
			want:        []destregistry.ValidationErrorDetail{{Field: "credentials.workload_identity_provider", Type: "forbidden"}},
		},
		{
			name:   "unknown auth method",
			config: map[string]string{"auth_method": "api_key"},
			want:   []destregistry.ValidationErrorDetail{{Field: "config.auth_method", Type: "enum"}},
		},
		{
			name:        "tenant id too long for the subject",
			tenantID:    strings.Repeat("t", 121),
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testProvider},
			want:        []destregistry.ValidationErrorDetail{{Field: "tenant_id", Type: "maxlength"}},
		},
		{
			name:        "longest tenant id",
			tenantID:    strings.Repeat("t", 120),
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testProvider},
		},
		{
			name:        "tenant id with a quote",
			tenantID:    "acme'corp",
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testProvider},
			want:        []destregistry.ValidationErrorDetail{{Field: "tenant_id", Type: "pattern"}},
		},
		{
			name:        "tenant id with a slash",
			tenantID:    "acme/corp",
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testProvider},
			want:        []destregistry.ValidationErrorDetail{{Field: "tenant_id", Type: "pattern"}},
		},
		{
			name:        "tenant id with a space",
			tenantID:    "acme corp",
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testProvider},
			want:        []destregistry.ValidationErrorDetail{{Field: "tenant_id", Type: "pattern"}},
		},
		{
			name:        "tenant id with allowed punctuation",
			tenantID:    "org_1.team-a:prod@eu",
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testProvider},
		},
		{
			name:        "tenant id with a quote and a service account key",
			tenantID:    "acme'corp",
			credentials: map[string]string{"service_account_json": serviceAccountJSON},
		},
		{
			name:        "empty service account key with workload identity",
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testProvider, "service_account_json": ""},
		},
		{
			name:        "long tenant id with service account key",
			tenantID:    strings.Repeat("t", 200),
			credentials: map[string]string{"service_account_json": serviceAccountJSON},
		},
		{
			name:        "disabled: no auth method",
			disabled:    true,
			credentials: map[string]string{"service_account_json": serviceAccountJSON},
		},
		{
			name:        "disabled: service account key",
			disabled:    true,
			config:      map[string]string{"auth_method": "service_account_key"},
			credentials: map[string]string{"service_account_json": serviceAccountJSON},
		},
		{
			name:        "disabled: workload identity rejected",
			disabled:    true,
			config:      map[string]string{"auth_method": "workload_identity"},
			credentials: map[string]string{"workload_identity_provider": testProvider, "service_account_json": serviceAccountJSON},
			want:        []destregistry.ValidationErrorDetail{{Field: "config.auth_method", Type: "enum"}},
		},
	}
	issuer := newTestIssuer(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []destgcppubsub.Option
			if !tt.disabled {
				opts = append(opts, destgcppubsub.WithWorkloadIdentity(issuer))
			}
			provider, err := destgcppubsub.New(testutil.Registry.MetadataLoader(), nil, opts...)
			require.NoError(t, err)

			tenantID := tt.tenantID
			if tenantID == "" {
				tenantID = "tenant_1"
			}
			err = provider.Validate(context.Background(), gcpDestination(tenantID, tt.config, tt.credentials))
			if tt.want == nil {
				assert.NoError(t, err)
				return
			}
			var verr *destregistry.ErrDestinationValidation
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, tt.want, verr.Errors)
		})
	}
}

// fakeGoogle stands in for Google STS and IAM Credentials.
type fakeGoogle struct {
	t        *testing.T
	issuer   *workloadidentity.Issuer
	server   *httptest.Server
	stsForm  chan map[string]string
	iamCalls chan *http.Request
	stsError string
}

func newFakeGoogle(t *testing.T, issuer *workloadidentity.Issuer) *fakeGoogle {
	f := &fakeGoogle{t: t, issuer: issuer, stsForm: make(chan map[string]string, 10), iamCalls: make(chan *http.Request, 10)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		form := map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		f.stsForm <- form
		w.Header().Set("Content-Type", "application/json")
		if f.stsError != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(f.stsError))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":      "federated-token",
			"issued_token_type": "urn:ietf:params:oauth:token-type:access_token",
			"token_type":        "Bearer",
			"expires_in":        3600,
		})
	})
	mux.HandleFunc("POST /v1/projects/-/serviceAccounts/{account}", func(w http.ResponseWriter, r *http.Request) {
		f.iamCalls <- r
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accessToken": "impersonated-token",
			"expireTime":  time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGoogle) provider(t *testing.T) *destgcppubsub.GCPPubSubDestination {
	t.Helper()
	p, err := destgcppubsub.New(testutil.Registry.MetadataLoader(), nil,
		destgcppubsub.WithWorkloadIdentity(f.issuer),
		destgcppubsub.WithGoogleEndpoints(f.server.URL+"/v1/token", f.server.URL),
	)
	require.NoError(t, err)
	return p
}

// verifySubjectToken checks the token the way Google does: signature against
// the issuer's JWKS, then the claims.
func (f *fakeGoogle) verifySubjectToken(raw string) *jwt.RegisteredClaims {
	t := f.t
	jwk := f.issuer.JWKS().Keys[0]
	dec := func(s string) *big.Int {
		b, err := base64.RawURLEncoding.DecodeString(s)
		require.NoError(t, err)
		return new(big.Int).SetBytes(b)
	}
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: dec(jwk.X), Y: dec(jwk.Y)}
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return pub, nil },
		jwt.WithValidMethods([]string{"ES256"}))
	require.NoError(t, err)
	assert.Equal(t, jwk.Kid, token.Header["kid"])
	return claims
}

func TestWorkloadIdentityTokenSource(t *testing.T) {
	t.Parallel()

	t.Run("exchanges a token for the destination's tenant", func(t *testing.T) {
		t.Parallel()
		google := newFakeGoogle(t, newTestIssuer(t))
		dest := gcpDestination("tenant_a",
			map[string]string{"auth_method": "workload_identity"},
			map[string]string{"workload_identity_provider": testProvider})

		ts, err := google.provider(t).WorkloadIdentityTokenSource(dest)
		require.NoError(t, err)
		token, err := ts.Token()
		require.NoError(t, err)
		assert.Equal(t, "federated-token", token.AccessToken)

		form := <-google.stsForm
		assert.Equal(t, "urn:ietf:params:oauth:grant-type:token-exchange", form["grant_type"])
		assert.Equal(t, testAudience, form["audience"])
		assert.Equal(t, "urn:ietf:params:oauth:token-type:jwt", form["subject_token_type"])
		assert.Equal(t, "https://www.googleapis.com/auth/cloud-platform", form["scope"])

		claims := google.verifySubjectToken(form["subject_token"])
		assert.Equal(t, "https://outpost.example.com/workload-identity", claims.Issuer)
		assert.Equal(t, "tenant:tenant_a", claims.Subject)
		assert.Equal(t, jwt.ClaimStrings{testAudience}, claims.Audience)
		assert.Equal(t, 5*time.Minute, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
	})

	t.Run("accepts the provider's full resource name", func(t *testing.T) {
		t.Parallel()
		google := newFakeGoogle(t, newTestIssuer(t))
		dest := gcpDestination("tenant_a",
			map[string]string{"auth_method": "workload_identity"},
			map[string]string{"workload_identity_provider": testAudience})

		ts, err := google.provider(t).WorkloadIdentityTokenSource(dest)
		require.NoError(t, err)
		_, err = ts.Token()
		require.NoError(t, err)
		assert.Equal(t, testAudience, (<-google.stsForm)["audience"])
	})

	t.Run("impersonates the service account", func(t *testing.T) {
		t.Parallel()
		google := newFakeGoogle(t, newTestIssuer(t))
		dest := gcpDestination("tenant_a",
			map[string]string{"auth_method": "workload_identity"},
			map[string]string{
				"workload_identity_provider": testProvider,
				"service_account_email":      "publisher@my-project.iam.gserviceaccount.com",
			})

		ts, err := google.provider(t).WorkloadIdentityTokenSource(dest)
		require.NoError(t, err)
		token, err := ts.Token()
		require.NoError(t, err)
		assert.Equal(t, "impersonated-token", token.AccessToken)

		<-google.stsForm
		call := <-google.iamCalls
		assert.Equal(t, "/v1/projects/-/serviceAccounts/publisher@my-project.iam.gserviceaccount.com:generateAccessToken", call.URL.Path)
		assert.Equal(t, "Bearer federated-token", call.Header.Get("Authorization"))
	})

	t.Run("surfaces the exchange error", func(t *testing.T) {
		t.Parallel()
		google := newFakeGoogle(t, newTestIssuer(t))
		google.stsError = `{"error":"invalid_grant","error_description":"The given credential is rejected by the attribute condition."}`
		dest := gcpDestination("tenant_a",
			map[string]string{"auth_method": "workload_identity"},
			map[string]string{"workload_identity_provider": testProvider})

		ts, err := google.provider(t).WorkloadIdentityTokenSource(dest)
		require.NoError(t, err)
		_, err = ts.Token()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rejected by the attribute condition")
	})
}

func TestCreatePublisher_WorkloadIdentity(t *testing.T) {
	t.Parallel()
	provider, err := destgcppubsub.New(testutil.Registry.MetadataLoader(), nil, destgcppubsub.WithWorkloadIdentity(newTestIssuer(t)))
	require.NoError(t, err)

	publisher, err := provider.CreatePublisher(context.Background(), gcpDestination("tenant_a",
		map[string]string{"auth_method": "workload_identity"},
		map[string]string{"workload_identity_provider": testProvider}))
	require.NoError(t, err)
	require.NoError(t, publisher.Close())
}

func TestObfuscateDestination_AlwaysReturnsServiceAccountJSON(t *testing.T) {
	t.Parallel()
	provider, err := destgcppubsub.New(testutil.Registry.MetadataLoader(), nil, destgcppubsub.WithWorkloadIdentity(newTestIssuer(t)))
	require.NoError(t, err)

	wif := gcpDestination("tenant_a",
		map[string]string{"auth_method": "workload_identity"},
		map[string]string{"workload_identity_provider": testProvider})
	got := provider.ObfuscateDestination(wif)
	assert.Equal(t, map[string]string{"service_account_json": "", "workload_identity_provider": testProvider}, map[string]string(got.Credentials))
	assert.NotContains(t, wif.Credentials, "service_account_json", "the stored destination is not modified")

	key := gcpDestination("tenant_a", nil, map[string]string{"service_account_json": `{"type":"service_account","project_id":"my-project"}`})
	assert.Equal(t, `{"ty`, provider.ObfuscateDestination(key).Credentials["service_account_json"][:4])
	assert.NotEqual(t, key.Credentials["service_account_json"], provider.ObfuscateDestination(key).Credentials["service_account_json"])
}

// A failed token exchange reaches the delivery attempt with Google's error.
func TestPublishEvent_WorkloadIdentityExchangeFailure(t *testing.T) {
	t.Parallel()
	google := newFakeGoogle(t, newTestIssuer(t))
	google.stsError = `{"error":"invalid_grant","error_description":"The given credential is rejected by the attribute condition."}`

	// The exchange happens before the RPC is sent: the server only needs to
	// accept a TLS connection.
	addr, clientTLS := startTLSGRPCServer(t)

	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	provider, err := destgcppubsub.New(testutil.Registry.MetadataLoader(), nil,
		destgcppubsub.WithWorkloadIdentity(google.issuer),
		destgcppubsub.WithGoogleEndpoints(google.server.URL+"/v1/token", google.server.URL),
		destgcppubsub.WithClientOptions(
			option.WithEndpoint(addr),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(credentials.NewTLS(clientTLS))),
		),
	)
	require.NoError(t, err)
	require.NoError(t, registry.RegisterProvider("gcp_pubsub", provider))

	dest := gcpDestination("tenant_a",
		map[string]string{"auth_method": "workload_identity"},
		map[string]string{"workload_identity_provider": testProvider})
	event := testutil.EventFactory.Any(testutil.EventFactory.WithTenantID("tenant_a"))

	attempt, err := registry.PublishEvent(context.Background(), dest, &event)
	require.Error(t, err)
	require.NotNil(t, attempt)
	assert.Equal(t, "failed", attempt.Status)
	assert.Equal(t, "ERR", attempt.Code)
	assert.Contains(t, attempt.ResponseData["error"], "code = Unauthenticated")
	assert.Contains(t, attempt.ResponseData["error"], "rejected by the attribute condition")

	var pubErr *destregistry.ErrDestinationPublishAttempt
	require.ErrorAs(t, err, &pubErr)
	assert.Equal(t, "publish_failed", pubErr.Data["error"])
}

func startTLSGRPCServer(t *testing.T) (string, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	})))
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return lis.Addr().String(), &tls.Config{RootCAs: pool}
}

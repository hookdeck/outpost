package workloadidentity_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hookdeck/outpost/internal/workloadidentity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testIssuer = "https://outpost.example.com/workload-identity"

func rsaPEM(t *testing.T, bits int) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func ecPEM(t *testing.T, curve elliptic.Curve) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
}

// rfc7638PublicPEM is the RSA key from RFC 7638 section 3.1, whose thumbprint
// is given there.
func rfc7638PublicPEM(t *testing.T) (string, string) {
	t.Helper()
	n, err := base64.RawURLEncoding.DecodeString("0vx7agoebGcQSuuPiLJXZptN9nndrQmbXEps2aiAFbWhM78LhWx4cbbfAAtVT86zwu1RK7aPFFxuhDR1L6tSoc_BJECPebWKRXjBZCiFV4n3oknjhMstn64tZ_2W-5JsGY4Hc5n9yBXArwl93lqt7_RN5w6Cf0h4QyQ5v-65YGjQR0_FDW2QvzqY368QQMicAtaSqzs8KJZgnYb9c7d0zgdAZHzu6qMQvRL5hajrn1n91CbOpbISD08qNLyrdkt-bFTWhAI4vMQFh6WeZu0fM4lFd2NcRwr3XPksINHaQ-G_xBniIqbw0Ls1jF44-csFCur-kEgU8awapJzKnqDKgw")
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&rsa.PublicKey{N: new(big.Int).SetBytes(n), E: 65537})
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), "NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs"
}

func TestNew_Validation(t *testing.T) {
	t.Parallel()
	rsaKey := rsaPEM(t, 2048)
	publicKey, _ := rfc7638PublicPEM(t)

	tests := []struct {
		name    string
		cfg     workloadidentity.Config
		wantErr string
	}{
		{name: "rsa", cfg: workloadidentity.Config{Issuer: testIssuer, SigningKey: rsaKey}},
		{name: "ec p-256", cfg: workloadidentity.Config{Issuer: testIssuer, SigningKey: ecPEM(t, elliptic.P256())}},
		{name: "http localhost", cfg: workloadidentity.Config{Issuer: "http://localhost:3333/workload-identity", SigningKey: rsaKey}},
		{name: "escaped newlines", cfg: workloadidentity.Config{Issuer: testIssuer, SigningKey: strings.ReplaceAll(rsaKey, "\n", `\n`)}},
		{name: "missing issuer", cfg: workloadidentity.Config{SigningKey: rsaKey}, wantErr: "issuer is required"},
		{name: "http issuer", cfg: workloadidentity.Config{Issuer: "http://outpost.example.com", SigningKey: rsaKey}, wantErr: "https"},
		{name: "relative issuer", cfg: workloadidentity.Config{Issuer: "outpost.example.com", SigningKey: rsaKey}, wantErr: "absolute"},
		{name: "trailing slash", cfg: workloadidentity.Config{Issuer: testIssuer + "/", SigningKey: rsaKey}, wantErr: "slash"},
		{name: "query", cfg: workloadidentity.Config{Issuer: testIssuer + "?a=b", SigningKey: rsaKey}, wantErr: "query"},
		{name: "missing key", cfg: workloadidentity.Config{Issuer: testIssuer}, wantErr: "signing key: expected exactly one"},
		{name: "two signing keys", cfg: workloadidentity.Config{Issuer: testIssuer, SigningKey: rsaKey + rsaKey}, wantErr: "expected exactly one"},
		{name: "public signing key", cfg: workloadidentity.Config{Issuer: testIssuer, SigningKey: publicKey}, wantErr: "must be a private key"},
		{name: "small rsa", cfg: workloadidentity.Config{Issuer: testIssuer, SigningKey: rsaPEM(t, 1024)}, wantErr: "at least 2048 bits"},
		{name: "ec p-384", cfg: workloadidentity.Config{Issuer: testIssuer, SigningKey: ecPEM(t, elliptic.P384())}, wantErr: "P-256"},
		{name: "garbage", cfg: workloadidentity.Config{Issuer: testIssuer, SigningKey: "not a key"}, wantErr: "invalid PEM"},
		{name: "bad verification key", cfg: workloadidentity.Config{Issuer: testIssuer, SigningKey: rsaKey, VerificationKeys: ecPEM(t, elliptic.P384())}, wantErr: "verification keys"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := workloadidentity.New(tt.cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestIssuer_DiscoveryAndJWKS(t *testing.T) {
	t.Parallel()
	previous, previousKid := rfc7638PublicPEM(t)
	ecKey := ecPEM(t, elliptic.P256())

	issuer, err := workloadidentity.New(workloadidentity.Config{
		Issuer:           testIssuer,
		SigningKey:       rsaPEM(t, 2048),
		VerificationKeys: previous + ecKey,
	})
	require.NoError(t, err)

	jwks := issuer.JWKS()
	require.Len(t, jwks.Keys, 3)
	assert.Equal(t, "RSA", jwks.Keys[0].Kty)
	assert.Equal(t, "RS256", jwks.Keys[0].Alg)
	assert.Equal(t, previousKid, jwks.Keys[1].Kid, "kid is the RFC 7638 thumbprint")
	assert.Equal(t, "AQAB", jwks.Keys[1].E)
	assert.Equal(t, "EC", jwks.Keys[2].Kty)
	assert.Equal(t, "ES256", jwks.Keys[2].Alg)
	assert.Equal(t, "P-256", jwks.Keys[2].Crv)
	for _, k := range jwks.Keys {
		assert.Equal(t, "sig", k.Use)
	}

	assert.Equal(t, workloadidentity.Discovery{
		Issuer:                           testIssuer,
		JWKSURI:                          testIssuer + "/jwks.json",
		IDTokenSigningAlgValuesSupported: []string{"RS256", "ES256"},
		SubjectTypesSupported:            []string{"public"},
		ResponseTypesSupported:           []string{"id_token"},
	}, issuer.Discovery())
}

func TestIssuer_JWKSDeduplicatesSigningKey(t *testing.T) {
	t.Parallel()
	key := rsaPEM(t, 2048)
	issuer, err := workloadidentity.New(workloadidentity.Config{Issuer: testIssuer, SigningKey: key, VerificationKeys: key})
	require.NoError(t, err)
	assert.Len(t, issuer.JWKS().Keys, 1)
}

func TestIssuer_Mint(t *testing.T) {
	t.Parallel()
	for name, key := range map[string]string{"rsa": rsaPEM(t, 2048), "ec": ecPEM(t, elliptic.P256())} {
		t.Run(name, func(t *testing.T) {
			issuer, err := workloadidentity.New(workloadidentity.Config{Issuer: testIssuer, SigningKey: key})
			require.NoError(t, err)

			audience := "//iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/p/providers/q"
			raw, err := issuer.Mint(workloadidentity.TenantSubject("t_1"), audience)
			require.NoError(t, err)

			claims := verify(t, issuer, raw)
			assert.Equal(t, testIssuer, claims.Issuer)
			assert.Equal(t, "tenant:t_1", claims.Subject)
			assert.Equal(t, jwt.ClaimStrings{audience}, claims.Audience)
			assert.WithinDuration(t, time.Now(), claims.IssuedAt.Time, 5*time.Second)
			assert.Equal(t, workloadidentity.TokenTTL, claims.ExpiresAt.Sub(claims.IssuedAt.Time))
		})
	}
}

func TestIssuer_MintRequiresSubjectAndAudience(t *testing.T) {
	t.Parallel()
	issuer, err := workloadidentity.New(workloadidentity.Config{Issuer: testIssuer, SigningKey: rsaPEM(t, 2048)})
	require.NoError(t, err)
	_, err = issuer.Mint("", "aud")
	assert.Error(t, err)
	_, err = issuer.Mint("tenant:t_1", "")
	assert.Error(t, err)
}

// verify checks the token against the issuer's published JWKS, the way a
// relying party would.
func verify(t *testing.T, issuer *workloadidentity.Issuer, raw string) *jwt.RegisteredClaims {
	t.Helper()
	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		for _, k := range issuer.JWKS().Keys {
			if k.Kid != kid {
				continue
			}
			return publicKey(t, k), nil
		}
		t.Fatalf("kid %q not in JWKS", kid)
		return nil, nil
	}, jwt.WithValidMethods([]string{"RS256", "ES256"}))
	require.NoError(t, err)
	return claims
}

func publicKey(t *testing.T, k workloadidentity.JWK) any {
	t.Helper()
	dec := func(s string) *big.Int {
		b, err := base64.RawURLEncoding.DecodeString(s)
		require.NoError(t, err)
		return new(big.Int).SetBytes(b)
	}
	switch k.Kty {
	case "RSA":
		return &rsa.PublicKey{N: dec(k.N), E: int(dec(k.E).Int64())}
	case "EC":
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: dec(k.X), Y: dec(k.Y)}
	}
	t.Fatalf("unexpected kty %q", k.Kty)
	return nil
}

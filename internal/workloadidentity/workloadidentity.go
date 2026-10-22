// Package workloadidentity issues short-lived OIDC tokens that identify a
// tenant to a cloud provider's workload identity federation (for example
// Google Cloud Workload Identity Federation). The provider trusts the
// configured issuer by fetching its discovery document and JWKS.
package workloadidentity

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// DiscoveryPath and JWKSPath are relative to the issuer URL.
	DiscoveryPath = "/.well-known/openid-configuration"
	JWKSPath      = "/jwks.json"

	// TokenTTL is the lifetime of a minted token. The token is only used to
	// obtain a provider access token, so it can be short.
	TokenTTL = 5 * time.Minute

	tenantSubjectPrefix = "tenant:"
)

// TenantSubject returns the token subject for a tenant.
func TenantSubject(tenantID string) string {
	return tenantSubjectPrefix + tenantID
}

// Config holds the issuer settings. SigningKey and VerificationKeys are PEM.
type Config struct {
	// Issuer is the public URL the tokens' "iss" claim carries and under
	// which DiscoveryPath and JWKSPath are served.
	Issuer string
	// SigningKey is the private key used to sign tokens (RSA of at least
	// 2048 bits or EC P-256).
	SigningKey string
	// VerificationKeys holds zero or more additional PEM keys (public or
	// private) whose public part is published in the JWKS, e.g. the previous
	// signing key during a rotation.
	VerificationKeys string
}

// Issuer mints tokens and describes itself for discovery.
type Issuer struct {
	url    string
	signer crypto.Signer
	method jwt.SigningMethod
	kid    string
	keys   []JWK
	algs   []string
}

// New validates the config and returns an Issuer.
func New(cfg Config) (*Issuer, error) {
	if err := validateIssuerURL(cfg.Issuer); err != nil {
		return nil, err
	}

	signingKeys, err := parsePEMKeys(cfg.SigningKey)
	if err != nil {
		return nil, fmt.Errorf("signing key: %w", err)
	}
	if len(signingKeys) != 1 {
		return nil, fmt.Errorf("signing key: expected exactly one PEM key, got %d", len(signingKeys))
	}
	signer, ok := signingKeys[0].(crypto.Signer)
	if !ok {
		return nil, errors.New("signing key: must be a private key")
	}

	verificationKeys, err := parsePEMKeys(cfg.VerificationKeys)
	if err != nil {
		return nil, fmt.Errorf("verification keys: %w", err)
	}

	issuer := &Issuer{
		url:    cfg.Issuer,
		signer: signer,
	}

	seen := map[string]bool{}
	for i, key := range append([]any{signer}, verificationKeys...) {
		jwk, method, err := publicJWK(key)
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("signing key: %w", err)
			}
			return nil, fmt.Errorf("verification keys: %w", err)
		}
		if i == 0 {
			issuer.kid = jwk.Kid
			issuer.method = method
		}
		if seen[jwk.Kid] {
			continue
		}
		seen[jwk.Kid] = true
		issuer.keys = append(issuer.keys, jwk)
		if !slices.Contains(issuer.algs, jwk.Alg) {
			issuer.algs = append(issuer.algs, jwk.Alg)
		}
	}

	return issuer, nil
}

// URL returns the issuer URL.
func (i *Issuer) URL() string {
	return i.url
}

// Mint returns a signed token for subject, valid for TokenTTL.
func (i *Issuer) Mint(subject, audience string) (string, error) {
	if subject == "" || audience == "" {
		return "", errors.New("workloadidentity: subject and audience are required")
	}
	now := time.Now()
	token := jwt.NewWithClaims(i.method, jwt.RegisteredClaims{
		Issuer:    i.url,
		Subject:   subject,
		Audience:  jwt.ClaimStrings{audience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(TokenTTL)),
	})
	token.Header["kid"] = i.kid
	return token.SignedString(i.signer)
}

// Discovery is the OpenID Provider metadata document.
type Discovery struct {
	Issuer                           string   `json:"issuer"`
	JWKSURI                          string   `json:"jwks_uri"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
	SubjectTypesSupported            []string `json:"subject_types_supported"`
	ResponseTypesSupported           []string `json:"response_types_supported"`
}

// Discovery returns the document served at DiscoveryPath.
func (i *Issuer) Discovery() Discovery {
	return Discovery{
		Issuer:                           i.url,
		JWKSURI:                          i.url + JWKSPath,
		IDTokenSigningAlgValuesSupported: i.algs,
		SubjectTypesSupported:            []string{"public"},
		ResponseTypesSupported:           []string{"id_token"},
	}
}

// JWKS is a JSON Web Key Set.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK is a public JSON Web Key (RSA or EC).
type JWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

// JWKS returns the document served at JWKSPath: the signing key first, then
// the verification keys.
func (i *Issuer) JWKS() JWKS {
	return JWKS{Keys: append([]JWK(nil), i.keys...)}
}

func validateIssuerURL(raw string) error {
	if raw == "" {
		return errors.New("issuer is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("issuer: %w", err)
	}
	if u.Host == "" {
		return errors.New("issuer: must be an absolute URL")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopback(u.Hostname()) {
			return errors.New("issuer: must use https (http is only allowed for localhost)")
		}
	default:
		return errors.New("issuer: must use https")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("issuer: must not contain a query, fragment or user info")
	}
	if strings.HasSuffix(raw, "/") {
		return errors.New("issuer: must not end with a slash")
	}
	return nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func parsePEMKeys(data string) ([]any, error) {
	rest := []byte(normalizePEM(data))
	var keys []any
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		key, err := parsePEMBlock(block)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if strings.TrimSpace(string(rest)) != "" {
		return nil, errors.New("invalid PEM data")
	}
	return keys, nil
}

// normalizePEM accepts PEM with literal "\n" sequences, as it often arrives
// through single-line environment variables.
func normalizePEM(data string) string {
	if !strings.Contains(data, "\n") && strings.Contains(data, `\n`) {
		return strings.ReplaceAll(data, `\n`, "\n")
	}
	return data
}

func parsePEMBlock(block *pem.Block) (any, error) {
	switch block.Type {
	case "PRIVATE KEY":
		return x509.ParsePKCS8PrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PUBLIC KEY":
		return x509.ParsePKIXPublicKey(block.Bytes)
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported PEM block %q", block.Type)
	}
}

// publicJWK returns the public JWK for key and the signing method its type
// implies.
func publicJWK(key any) (JWK, jwt.SigningMethod, error) {
	if signer, ok := key.(crypto.Signer); ok {
		key = signer.Public()
	}
	switch pub := key.(type) {
	case *rsa.PublicKey:
		if pub.N.BitLen() < 2048 {
			return JWK{}, nil, fmt.Errorf("RSA key must be at least 2048 bits, got %d", pub.N.BitLen())
		}
		n := b64(pub.N.Bytes())
		e := b64(big.NewInt(int64(pub.E)).Bytes())
		kid := thumbprint(fmt.Sprintf(`{"e":%q,"kty":"RSA","n":%q}`, e, n))
		return JWK{Kty: "RSA", Use: "sig", Alg: "RS256", Kid: kid, N: n, E: e}, jwt.SigningMethodRS256, nil
	case *ecdsa.PublicKey:
		if pub.Curve != elliptic.P256() {
			return JWK{}, nil, errors.New("EC key must use the P-256 curve")
		}
		raw, err := pub.Bytes() // uncompressed point: 0x04 || X || Y
		if err != nil {
			return JWK{}, nil, err
		}
		x, y := b64(raw[1:33]), b64(raw[33:65])
		kid := thumbprint(fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":%q,"y":%q}`, x, y))
		return JWK{Kty: "EC", Use: "sig", Alg: "ES256", Kid: kid, Crv: "P-256", X: x, Y: y}, jwt.SigningMethodES256, nil
	default:
		return JWK{}, nil, fmt.Errorf("unsupported key type %T (use RSA or EC P-256)", key)
	}
}

// thumbprint computes the RFC 7638 JWK thumbprint from the key's canonical
// JSON (required members, lexicographic order, no whitespace).
func thumbprint(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return b64(sum[:])
}

func b64(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

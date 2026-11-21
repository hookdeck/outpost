package apirouter

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const issuer = "outpost"

var signingMethod = jwt.SigningMethodHS256

type jsonwebtoken struct{}

var JWT = jsonwebtoken{}

var (
	ErrInvalidToken = errors.New("invalid token")
	// ErrNoJWTSecret is returned by New when no signing secret is configured.
	// Tenant JWT auth is opt-in: with no secret, tokens are neither issued nor
	// accepted (Extract and Verify return ErrInvalidToken).
	ErrNoJWTSecret = errors.New("JWT secret not configured")
)

// JWTClaims contains the custom claims for JWT tokens
type JWTClaims struct {
	TenantID     string
	DeploymentID string
}

func (_ jsonwebtoken) New(jwtSecret string, claims JWTClaims) (string, error) {
	if jwtSecret == "" {
		return "", ErrNoJWTSecret
	}
	now := time.Now()
	mapClaims := jwt.MapClaims{
		"iss": issuer,
		"sub": claims.TenantID,
		"iat": now.Unix(),
		"exp": now.Add(24 * time.Hour).Unix(),
	}
	if claims.DeploymentID != "" {
		mapClaims["deployment_id"] = claims.DeploymentID
	}
	token := jwt.NewWithClaims(signingMethod, mapClaims)
	return token.SignedString([]byte(jwtSecret))
}

func (_ jsonwebtoken) Extract(jwtSecret string, tokenString string) (JWTClaims, error) {
	// An empty secret means tenant JWT auth is not configured. Without this
	// guard, golang-jwt would verify HMAC against an empty key, which anyone
	// can reproduce — a forged token for any tenant would be accepted.
	if jwtSecret == "" {
		return JWTClaims{}, ErrInvalidToken
	}
	token, err := jwt.Parse(
		tokenString,
		func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		},
		jwt.WithValidMethods([]string{signingMethod.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return JWTClaims{}, ErrInvalidToken
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return JWTClaims{}, ErrInvalidToken
	}

	tenantID, err := token.Claims.GetSubject()
	if err != nil {
		return JWTClaims{}, ErrInvalidToken
	}

	var deploymentID string
	if did, ok := claims["deployment_id"].(string); ok {
		deploymentID = did
	}

	return JWTClaims{
		TenantID:     tenantID,
		DeploymentID: deploymentID,
	}, nil
}

func (_ jsonwebtoken) Verify(jwtSecret string, tokenString string, tenantID string) (bool, error) {
	if jwtSecret == "" {
		return false, ErrInvalidToken
	}
	token, err := jwt.Parse(
		tokenString,
		func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		},
		jwt.WithValidMethods([]string{signingMethod.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithSubject(tenantID),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return false, ErrInvalidToken
	}
	return true, nil
}

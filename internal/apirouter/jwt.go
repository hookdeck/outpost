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
)

// JWTClaims contains the custom claims for JWT tokens
type JWTClaims struct {
	TenantID     string
	DeploymentID string
}

func (_ jsonwebtoken) New(jwtSecret string, claims JWTClaims) (string, error) {
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
	token, err := jwt.Parse(
		tokenString,
		func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		},
		jwt.WithIssuer(issuer),
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
	token, err := jwt.Parse(
		tokenString,
		func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		},
		jwt.WithIssuer(issuer),
		jwt.WithSubject(tenantID),
	)
	if err != nil || !token.Valid {
		return false, ErrInvalidToken
	}
	return true, nil
}

package api

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

func (_ jsonwebtoken) New(jwtSecret string, tenantID string) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(signingMethod, jwt.MapClaims{
		"iss": issuer,
		"sub": tenantID,
		"iat": now.Unix(),
		"exp": now.Add(24 * time.Hour).Unix(),
	})
	return token.SignedString([]byte(jwtSecret))
}

func (_ jsonwebtoken) ExtractTenantID(jwtSecret string, tokenString string) (string, error) {
	token, err := jwt.Parse(
		tokenString,
		func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		},
		jwt.WithIssuer(issuer),
	)
	if err != nil || !token.Valid {
		return "", ErrInvalidToken
	}

	tenantID, err := token.Claims.GetSubject()
	if err != nil {
		return "", ErrInvalidToken
	}
	return tenantID, nil
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

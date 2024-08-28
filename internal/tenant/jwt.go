package tenant

import (
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

const issuer = "eventkit"

var signingMethod = jwt.SigningMethodHS256

type jsonwebtoken struct{}

var JWT = jsonwebtoken{}

func (_ jsonwebtoken) New(jwtKey string, tenantID string) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(signingMethod, jwt.MapClaims{
		"iss": issuer,
		"sub": tenantID,
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})
	return token.SignedString([]byte(jwtKey))
}

func (_ jsonwebtoken) Verify(jwtKey string, tokenString string, tenantID string) (bool, error) {
	token, err := jwt.Parse(
		tokenString,
		func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtKey), nil
		},
		jwt.WithIssuer(issuer),
		jwt.WithSubject(tenantID),
	)
	if err != nil {
		return false, err
	}
	return token.Valid, nil
}

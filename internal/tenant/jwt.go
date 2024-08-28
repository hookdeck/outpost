package tenant

import (
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

const issuer = "eventkit"

var signingMethod = jwt.SigningMethodHS256

type jsonwebtoken struct{}

var JWT = jsonwebtoken{}

func (_ jsonwebtoken) New(jwtSecret string, tenantID string) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(signingMethod, jwt.MapClaims{
		"iss": issuer,
		"sub": tenantID,
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	})
	return token.SignedString([]byte(jwtSecret))
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
	if err != nil {
		return false, err
	}
	return token.Valid, nil
}

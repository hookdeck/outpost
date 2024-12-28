package api_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	api "github.com/hookdeck/outpost/internal/services/api"
	"github.com/stretchr/testify/assert"
)

func TestJWT(t *testing.T) {
	t.Parallel()

	const issuer = "outpost"
	const jwtKey = "supersecret"
	const tenantID = "tenantID"
	var signingMethod = jwt.SigningMethodHS256

	t.Run("should generate a new jwt token", func(t *testing.T) {
		t.Parallel()
		token, err := api.JWT.New(jwtKey, tenantID)
		assert.Nil(t, err)
		assert.NotEqual(t, "", token)
	})

	t.Run("should verify a valid jwt token", func(t *testing.T) {
		t.Parallel()
		token, err := api.JWT.New(jwtKey, tenantID)
		if err != nil {
			t.Fatal(err)
		}
		valid, err := api.JWT.Verify(jwtKey, token, tenantID)
		assert.Nil(t, err)
		assert.True(t, valid)
	})

	t.Run("should extract tenantID from valid token", func(t *testing.T) {
		t.Parallel()
		token, err := api.JWT.New(jwtKey, tenantID)
		if err != nil {
			t.Fatal(err)
		}
		extractedTenantID, err := api.JWT.ExtractTenantID(jwtKey, token)
		assert.Nil(t, err)
		assert.Equal(t, tenantID, extractedTenantID)
	})

	t.Run("should fail to extract tenantID from invalid token", func(t *testing.T) {
		t.Parallel()
		_, err := api.JWT.ExtractTenantID(jwtKey, "invalid_token")
		assert.ErrorIs(t, err, api.ErrInvalidToken)
	})

	t.Run("should fail to extract tenantID from token with invalid issuer", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		jwtToken := jwt.NewWithClaims(signingMethod, jwt.MapClaims{
			"iss": "not-outpost",
			"sub": tenantID,
			"iat": now.Unix(),
			"exp": now.Add(24 * time.Hour).Unix(),
		})
		token, err := jwtToken.SignedString([]byte(jwtKey))
		if err != nil {
			t.Fatal(err)
		}
		_, err = api.JWT.ExtractTenantID(jwtKey, token)
		assert.ErrorIs(t, err, api.ErrInvalidToken)
	})

	t.Run("should reject an invalid token", func(t *testing.T) {
		t.Parallel()
		valid, err := api.JWT.Verify(jwtKey, "invalid_token", tenantID)
		assert.ErrorIs(t, err, api.ErrInvalidToken)
		assert.False(t, valid)
	})

	t.Run("should reject a token from a different issuer", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		jwtToken := jwt.NewWithClaims(signingMethod, jwt.MapClaims{
			"iss": "not-outpost",
			"sub": tenantID,
			"iat": now.Unix(),
			"exp": now.Add(24 * time.Hour).Unix(),
		})
		token, err := jwtToken.SignedString([]byte(jwtKey))
		if err != nil {
			t.Fatal(err)
		}
		valid, err := api.JWT.Verify(jwtKey, token, tenantID)
		assert.ErrorIs(t, err, api.ErrInvalidToken)
		assert.False(t, valid)
	})

	t.Run("should reject a token for a different tenant", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		jwtToken := jwt.NewWithClaims(signingMethod, jwt.MapClaims{
			"iss": issuer,
			"sub": "different_tenantID",
			"iat": now.Unix(),
			"exp": now.Add(24 * time.Hour).Unix(),
		})
		token, err := jwtToken.SignedString([]byte(jwtKey))
		if err != nil {
			t.Fatal(err)
		}
		valid, err := api.JWT.Verify(jwtKey, token, tenantID)
		assert.ErrorIs(t, err, api.ErrInvalidToken)
		assert.False(t, valid)
	})

	t.Run("should reject an expired token", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		jwtToken := jwt.NewWithClaims(signingMethod, jwt.MapClaims{
			"iss": issuer,
			"sub": tenantID,
			"iat": now.Add(-2 * time.Hour).Unix(),
			"exp": now.Add(-24 * time.Hour).Unix(),
		})
		token, err := jwtToken.SignedString([]byte(jwtKey))
		if err != nil {
			t.Fatal(err)
		}
		valid, err := api.JWT.Verify(jwtKey, token, tenantID)
		assert.ErrorIs(t, err, api.ErrInvalidToken)
		assert.False(t, valid)
	})
}

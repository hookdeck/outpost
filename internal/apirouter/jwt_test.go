package apirouter_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"

	"github.com/hookdeck/outpost/internal/apirouter"
)

func TestJWT(t *testing.T) {
	t.Parallel()

	const issuer = "outpost"
	const jwtKey = "supersecret"
	const tenantID = "tenantID"
	const deploymentID = "deployment123"
	var signingMethod = jwt.SigningMethodHS256

	t.Run("should generate a new jwt token", func(t *testing.T) {
		t.Parallel()
		token, err := apirouter.JWT.New(jwtKey, apirouter.JWTClaims{TenantID: tenantID})
		assert.Nil(t, err)
		assert.NotEqual(t, "", token)
	})

	t.Run("should generate a new jwt token with deployment_id", func(t *testing.T) {
		t.Parallel()
		token, err := apirouter.JWT.New(jwtKey, apirouter.JWTClaims{
			TenantID:     tenantID,
			DeploymentID: deploymentID,
		})
		assert.Nil(t, err)
		assert.NotEqual(t, "", token)

		// Verify deployment_id is in the token
		claims, err := apirouter.JWT.Extract(jwtKey, token)
		assert.Nil(t, err)
		assert.Equal(t, deploymentID, claims.DeploymentID)
	})

	t.Run("should verify a valid jwt token", func(t *testing.T) {
		t.Parallel()
		token, err := apirouter.JWT.New(jwtKey, apirouter.JWTClaims{TenantID: tenantID})
		if err != nil {
			t.Fatal(err)
		}
		valid, err := apirouter.JWT.Verify(jwtKey, token, tenantID)
		assert.Nil(t, err)
		assert.True(t, valid)
	})

	t.Run("should extract claims from valid token", func(t *testing.T) {
		t.Parallel()
		token, err := apirouter.JWT.New(jwtKey, apirouter.JWTClaims{TenantID: tenantID})
		if err != nil {
			t.Fatal(err)
		}
		claims, err := apirouter.JWT.Extract(jwtKey, token)
		assert.Nil(t, err)
		assert.Equal(t, tenantID, claims.TenantID)
		assert.Equal(t, "", claims.DeploymentID)
	})

	t.Run("should fail to extract claims from invalid token", func(t *testing.T) {
		t.Parallel()
		_, err := apirouter.JWT.Extract(jwtKey, "invalid_token")
		assert.ErrorIs(t, err, apirouter.ErrInvalidToken)
	})

	t.Run("should extract all claims from valid token", func(t *testing.T) {
		t.Parallel()
		token, err := apirouter.JWT.New(jwtKey, apirouter.JWTClaims{
			TenantID:     tenantID,
			DeploymentID: deploymentID,
		})
		if err != nil {
			t.Fatal(err)
		}
		claims, err := apirouter.JWT.Extract(jwtKey, token)
		assert.Nil(t, err)
		assert.Equal(t, tenantID, claims.TenantID)
		assert.Equal(t, deploymentID, claims.DeploymentID)
	})

	t.Run("should return empty deployment_id when not in token", func(t *testing.T) {
		t.Parallel()
		token, err := apirouter.JWT.New(jwtKey, apirouter.JWTClaims{TenantID: tenantID})
		if err != nil {
			t.Fatal(err)
		}
		claims, err := apirouter.JWT.Extract(jwtKey, token)
		assert.Nil(t, err)
		assert.Equal(t, "", claims.DeploymentID)
	})

	t.Run("should fail to extract claims from token with invalid issuer", func(t *testing.T) {
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
		_, err = apirouter.JWT.Extract(jwtKey, token)
		assert.ErrorIs(t, err, apirouter.ErrInvalidToken)
	})

	t.Run("should reject an invalid token", func(t *testing.T) {
		t.Parallel()
		valid, err := apirouter.JWT.Verify(jwtKey, "invalid_token", tenantID)
		assert.ErrorIs(t, err, apirouter.ErrInvalidToken)
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
		valid, err := apirouter.JWT.Verify(jwtKey, token, tenantID)
		assert.ErrorIs(t, err, apirouter.ErrInvalidToken)
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
		valid, err := apirouter.JWT.Verify(jwtKey, token, tenantID)
		assert.ErrorIs(t, err, apirouter.ErrInvalidToken)
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
		valid, err := apirouter.JWT.Verify(jwtKey, token, tenantID)
		assert.ErrorIs(t, err, apirouter.ErrInvalidToken)
		assert.False(t, valid)
	})
}

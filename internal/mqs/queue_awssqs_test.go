package mqs_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSSQSConfig_ToCredentials(t *testing.T) {
	t.Parallel()

	t.Run("valid static credentials", func(t *testing.T) {
		t.Parallel()
		cfg := &mqs.AWSSQSConfig{ServiceAccountCredentials: "AKID:SECRET:"}
		creds, err := cfg.ToCredentials()
		require.NoError(t, err)
		require.NotNil(t, creds)

		value, err := creds.Retrieve(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "AKID", value.AccessKeyID)
		assert.Equal(t, "SECRET", value.SecretAccessKey)
		assert.Empty(t, value.SessionToken)
	})

	t.Run("valid static credentials with session token", func(t *testing.T) {
		t.Parallel()
		cfg := &mqs.AWSSQSConfig{ServiceAccountCredentials: "AKID:SECRET:TOKEN"}
		creds, err := cfg.ToCredentials()
		require.NoError(t, err)
		require.NotNil(t, creds)

		value, err := creds.Retrieve(t.Context())
		require.NoError(t, err)
		assert.Equal(t, "TOKEN", value.SessionToken)
	})

	t.Run("empty string defers to default credential chain", func(t *testing.T) {
		t.Parallel()
		cfg := &mqs.AWSSQSConfig{ServiceAccountCredentials: ""}
		creds, err := cfg.ToCredentials()
		require.NoError(t, err)
		assert.Nil(t, creds, "empty credentials should return nil so the SDK default chain is used")
	})

	t.Run("all-empty parts defers to default credential chain", func(t *testing.T) {
		t.Parallel()
		cfg := &mqs.AWSSQSConfig{ServiceAccountCredentials: "::"}
		creds, err := cfg.ToCredentials()
		require.NoError(t, err)
		assert.Nil(t, creds, "\"::\" should be treated as no credentials")
	})

	t.Run("partial credentials still build a static provider", func(t *testing.T) {
		t.Parallel()
		cfg := &mqs.AWSSQSConfig{ServiceAccountCredentials: "AKID::"}
		creds, err := cfg.ToCredentials()
		require.NoError(t, err)
		require.NotNil(t, creds)
	})

	t.Run("malformed non-empty credentials error", func(t *testing.T) {
		t.Parallel()
		cfg := &mqs.AWSSQSConfig{ServiceAccountCredentials: "AKID:SECRET"}
		creds, err := cfg.ToCredentials()
		require.Error(t, err)
		assert.Nil(t, creds)
	})
}

package mcpevents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeriveSubscriptionID_GoldenVectors(t *testing.T) {
	t.Parallel()
	type vector struct {
		TenantID     string `json:"tenant"`
		URL          string `json:"url"`
		Name         string `json:"name"`
		Arguments    string `json:"arguments"`
		CanonicalKey string `json:"canonical_key"`
		ID           string `json:"id"`
	}
	for _, v := range loadVectors[vector](t, "subscription_ids.json") {
		args, err := CanonicalizeJSON([]byte(v.Arguments))
		require.NoError(t, err)
		assert.Equal(t, v.ID, DeriveSubscriptionID(v.TenantID, v.URL, v.Name, args), "tenant %q", v.TenantID)

		var parsedArgs any
		require.NoError(t, json.Unmarshal([]byte(v.Arguments), &parsedArgs))
		key, err := CanonicalJSON(map[string]any{"tenant": v.TenantID, "url": v.URL, "name": v.Name, "arguments": parsedArgs})
		require.NoError(t, err)
		assert.Equal(t, v.CanonicalKey, string(key))

		sum := sha256.Sum256([]byte(v.CanonicalKey))
		assert.Equal(t, v.ID, "sub_"+hex.EncodeToString(sum[:16]))
	}
}

func TestDeriveSubscriptionID(t *testing.T) {
	t.Parallel()
	id := DeriveSubscriptionID("t1", "https://a.example/", "t", json.RawMessage(`{}`))
	assert.Equal(t, id, DeriveSubscriptionID("t1", "https://a.example/", "t", nil), "nil arguments mean {}")
	assert.Len(t, id, len("sub_")+32)
	assert.True(t, strings.HasPrefix(id, "sub_"))
	for _, other := range []string{
		DeriveSubscriptionID("t2", "https://a.example/", "t", nil),
		DeriveSubscriptionID("t1", "https://b.example/", "t", nil),
		DeriveSubscriptionID("t1", "https://a.example/", "u", nil),
		DeriveSubscriptionID("t1", "https://a.example/", "t", json.RawMessage(`{"a":1}`)),
	} {
		assert.NotEqual(t, id, other)
	}
	assert.NotEqual(t,
		DeriveSubscriptionID(`a","url":"x`, "y", "t", nil),
		DeriveSubscriptionID("a", "x", "t", nil))
}

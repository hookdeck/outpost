package mcpevents

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSign_StandardWebhooksVector(t *testing.T) {
	t.Parallel()
	// The Standard Webhooks reference test vector.
	key, err := base64.StdEncoding.DecodeString("MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw")
	require.NoError(t, err)
	got := Sign("msg_p5jXN8AQM9LWM0D4loKWxJek", time.Unix(1614265330, 0), []byte(`{"test": 2432232314}`), [][]byte{key})
	assert.Equal(t, "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=", got)
}

func TestSign_VerifiesWithStandardWebhooksLibrary(t *testing.T) {
	t.Parallel()
	_, current := secretOf(32, base64.StdEncoding)
	_, previous := secretOf(24, base64.StdEncoding)
	previous[0] ^= 0xff
	body := []byte(`{"eventId":"evt_1","name":"order.created","timestamp":"2026-10-09T16:58:12Z","data":{"a":"<&>"},"cursor":null}`)
	now := time.Now()

	t.Run("single key", func(t *testing.T) {
		h := http.Header{}
		require.NoError(t, SetHeaders(h, "evt_1", "sub_x", now, body, [][]byte{current}))
		assert.Equal(t, 1, strings.Count(canonicalHeader(h).Get(HeaderWebhookSignature), "v1,"))
		verifyWith(t, current, body, h, true)
		verifyWith(t, previous, body, h, false)
	})

	t.Run("dual keys during rotation", func(t *testing.T) {
		h := http.Header{}
		require.NoError(t, SetHeaders(h, "evt_1", "sub_x", now, body, [][]byte{current, previous}))
		sig := canonicalHeader(h).Get(HeaderWebhookSignature)
		parts := strings.Split(sig, " ")
		require.Len(t, parts, 2, "space-separated")
		for _, p := range parts {
			assert.True(t, strings.HasPrefix(p, "v1,"))
		}
		verifyWith(t, current, body, h, true)
		verifyWith(t, previous, body, h, true)
		// Order follows the keys: current first.
		assert.Equal(t, Sign("evt_1", now, body, [][]byte{current}), parts[0])
	})

	t.Run("tampered body", func(t *testing.T) {
		h := http.Header{}
		require.NoError(t, SetHeaders(h, "evt_1", "sub_x", now, body, [][]byte{current}))
		verifyWith(t, current, append([]byte(nil), append(body, ' ')...), h, false)
	})
}

func verifyWith(t *testing.T, key, body []byte, h http.Header, ok bool) {
	t.Helper()
	wh, err := standardwebhooks.NewWebhookRaw(key)
	require.NoError(t, err)
	// Verify reads canonical keys, as a receiver's parsed request has them.
	err = wh.Verify(body, canonicalHeader(h))
	if ok {
		assert.NoError(t, err)
	} else {
		assert.Error(t, err)
	}
}

func TestSetHeaders(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	ts := time.Unix(1791576000, 999)
	require.NoError(t, SetHeaders(h, "evt_1", "sub_3f1c", ts, []byte(`{}`), [][]byte{[]byte("k")}))
	// The documented spellings, not http.Header's canonical ones: HTTP/1.1
	// sends map keys as they are, so every request Outpost signs (deliveries,
	// challenges, terminated envelopes) spells them the same way.
	assert.Equal(t, http.Header{
		"Content-Type":          {"application/json"},
		"webhook-id":            {"evt_1"},
		"webhook-timestamp":     {"1791576000"},
		"webhook-signature":     {Sign("evt_1", ts, []byte(`{}`), [][]byte{[]byte("k")})},
		"X-MCP-Subscription-Id": {"sub_3f1c"},
	}, h)

	t.Run("replaces canonical spellings", func(t *testing.T) {
		t.Parallel()
		h := http.Header{}
		h.Set(HeaderWebhookID, "old")
		h.Set(HeaderSubscriptionID, "old")
		h.Set(HeaderContentType, "text/plain")
		require.NoError(t, SetHeaders(h, "evt_1", "sub_3f1c", ts, []byte(`{}`), [][]byte{[]byte("k")}))
		assert.Len(t, h, 5, "one entry per header: %v", h)
		assert.Equal(t, []string{"evt_1"}, h[HeaderWebhookID])
		assert.Equal(t, []string{"sub_3f1c"}, h[HeaderSubscriptionID])
		assert.Equal(t, "application/json", h.Get(HeaderContentType))
	})

	assert.ErrorIs(t, SetHeaders(http.Header{}, "evt_1", "sub", ts, nil, nil), ErrNoSigningKey)
	assert.Empty(t, Sign("evt_1", ts, nil, nil))
}

func TestActiveKeys(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Second), now.Add(time.Second)
	cur, prev := []byte("current"), []byte("previous")

	assert.Equal(t, [][]byte{cur}, ActiveKeys([]Secret{{Key: cur}}, now))
	assert.Equal(t, [][]byte{cur, prev}, ActiveKeys([]Secret{{Key: cur}, {Key: prev, InvalidAt: &future}}, now))
	assert.Equal(t, [][]byte{cur}, ActiveKeys([]Secret{{Key: cur}, {Key: prev, InvalidAt: &past}}, now))
	assert.Equal(t, [][]byte{cur}, ActiveKeys([]Secret{{Key: cur}, {Key: prev, InvalidAt: &now}}, now), "invalid from InvalidAt on")
	assert.Empty(t, ActiveKeys([]Secret{{Key: nil}, {Key: prev, InvalidAt: &past}}, now))
	assert.Empty(t, ActiveKeys(nil, now))
}

package mcpevents

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// Delivery headers. MCP deliveries, verification challenges and terminated
// envelopes carry exactly these (plus the client's User-Agent); http.Header
// canonicalizes the names on the wire, which receivers match
// case-insensitively.
const (
	HeaderContentType      = "Content-Type"
	HeaderWebhookID        = "webhook-id"
	HeaderWebhookTimestamp = "webhook-timestamp"
	HeaderWebhookSignature = "webhook-signature"
	HeaderSubscriptionID   = "X-MCP-Subscription-Id"
)

// ErrNoSigningKey is returned when no secret is valid at signing time.
var ErrNoSigningKey = errors.New("mcpevents: no valid signing secret")

// Secret is one signing key: the current secret (InvalidAt nil) or, during
// rotation, the previous one until InvalidAt.
type Secret struct {
	Key       []byte
	InvalidAt *time.Time
}

// ActiveKeys returns the keys of secrets still valid at now, in order: a
// secret stops signing at its InvalidAt. Empty keys are skipped.
func ActiveKeys(secrets []Secret, now time.Time) [][]byte {
	keys := make([][]byte, 0, len(secrets))
	for _, s := range secrets {
		if len(s.Key) == 0 || (s.InvalidAt != nil && !now.Before(*s.InvalidAt)) {
			continue
		}
		keys = append(keys, s.Key)
	}
	return keys
}

// Sign returns the Standard Webhooks signature header value: one
// "v1,<base64 HMAC-SHA256(key, msgID.unix(ts).body)>" per key, space
// separated, in key order. It returns "" for no keys.
func Sign(msgID string, ts time.Time, body []byte, keys [][]byte) string {
	if len(keys) == 0 {
		return ""
	}
	prefix := make([]byte, 0, len(msgID)+24)
	prefix = append(prefix, msgID...)
	prefix = append(prefix, '.')
	prefix = strconv.AppendInt(prefix, ts.Unix(), 10)
	prefix = append(prefix, '.')

	const sigLen = 3 + 44 // "v1," + base64 of 32 bytes
	out := make([]byte, 0, len(keys)*(sigLen+1))
	var sum [sha256.Size]byte
	for i, key := range keys {
		if i > 0 {
			out = append(out, ' ')
		}
		mac := hmac.New(sha256.New, key)
		mac.Write(prefix)
		mac.Write(body)
		out = append(out, "v1,"...)
		out = base64.StdEncoding.AppendEncode(out, mac.Sum(sum[:0]))
	}
	return string(out)
}

// SetHeaders sets the delivery headers on h for body: Content-Type,
// webhook-id, webhook-timestamp (ts, Unix seconds), webhook-signature (every
// key) and X-MCP-Subscription-Id. It returns ErrNoSigningKey for no keys.
func SetHeaders(h http.Header, msgID, subscriptionID string, ts time.Time, body []byte, keys [][]byte) error {
	if len(keys) == 0 {
		return ErrNoSigningKey
	}
	h.Set(HeaderContentType, "application/json")
	h.Set(HeaderWebhookID, msgID)
	h.Set(HeaderWebhookTimestamp, strconv.FormatInt(ts.Unix(), 10))
	h.Set(HeaderWebhookSignature, Sign(msgID, ts, body, keys))
	h.Set(HeaderSubscriptionID, subscriptionID)
	return nil
}

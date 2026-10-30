package mcpreceiver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	secretPrefix  = "whsec_"
	signatureSkew = 5 * time.Minute
)

func signingKey(secret string) []byte {
	if raw, ok := strings.CutPrefix(secret, secretPrefix); ok {
		if key, err := base64.StdEncoding.DecodeString(raw); err == nil {
			return key
		}
	}
	return []byte(secret)
}

// Sign returns the Standard Webhooks "v1,<base64>" signature of a message.
func Sign(secret, id, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, signingKey(secret))
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// SignatureValues returns the v1 signatures of a webhook-signature header.
func SignatureValues(header http.Header) []string {
	var out []string
	for _, part := range strings.Fields(strings.Join(header.Values("webhook-signature"), " ")) {
		if strings.HasPrefix(part, "v1,") {
			out = append(out, part)
		}
	}
	return out
}

// VerifiedWith reports, per secret, whether some v1 signature of the
// request matches it.
func (r Request) VerifiedWith(secrets ...string) []bool {
	sigs := SignatureValues(r.Header)
	out := make([]bool, len(secrets))
	for i, secret := range secrets {
		want := Sign(secret, r.Header.Get("webhook-id"), r.Header.Get("webhook-timestamp"), r.Body)
		for _, sig := range sigs {
			if hmac.Equal([]byte(sig), []byte(want)) {
				out[i] = true
			}
		}
	}
	return out
}

// TimestampWithinSkew reports whether webhook-timestamp is within five
// minutes of the receipt time.
func (r Request) TimestampWithinSkew() bool {
	ts, err := strconv.ParseInt(r.Header.Get("webhook-timestamp"), 10, 64)
	if err != nil {
		return false
	}
	d := r.Received.Sub(time.Unix(ts, 0))
	if d < 0 {
		d = -d
	}
	return d <= signatureSkew
}

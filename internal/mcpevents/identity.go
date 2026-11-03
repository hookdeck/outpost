package mcpevents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const SubscriptionIDPrefix = "sub_"

// DeriveSubscriptionID returns "sub_" and the hex of the first 16 bytes of the
// SHA-256 of the canonical JSON {arguments, name, principal, url}. args must
// already be canonical JSON (empty means {}) and url normalized.
func DeriveSubscriptionID(principal, url, name string, args json.RawMessage) string {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	b := make([]byte, 0, 48+len(args)+len(name)+len(principal)+len(url))
	b = append(b, `{"arguments":`...)
	b = append(b, args...)
	b = append(b, `,"name":`...)
	b = appendJSString(b, name)
	b = append(b, `,"principal":`...)
	b = appendJSString(b, principal)
	b = append(b, `,"url":`...)
	b = appendJSString(b, url)
	b = append(b, '}')
	sum := sha256.Sum256(b)
	return SubscriptionIDPrefix + hex.EncodeToString(sum[:16])
}

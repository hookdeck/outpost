package mcpevents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// SubscriptionIDPrefix starts every derived subscription ID.
const SubscriptionIDPrefix = "sub_"

// DeriveSubscriptionID returns the subscription ID for a subscription key:
// "sub_" + the first 32 hex characters of
// sha256(canonical_json({principal, url, name, arguments})), the derivation
// the MCP Events guide uses. url must be normalized (NormalizeCallbackURL) and
// args canonical (ParseSubscribeParams); nil args means {}.
//
// The tenant is not part of the key, so IDs are unique per tenant only: the
// same principal, URL, name and arguments in two tenants derive the same ID
// (and the same X-MCP-Subscription-Id).
func DeriveSubscriptionID(principal, url, name string, args json.RawMessage) string {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	b := make([]byte, 0, 48+len(args)+len(name)+len(principal)+len(url))
	// Keys in canonical (sorted) order.
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

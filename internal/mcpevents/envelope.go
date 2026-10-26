package mcpevents

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

const (
	// MaxEnvelopeBytes is the largest body the mcp destination sends; larger
	// events fail as payload_too_large without a request.
	MaxEnvelopeBytes = 256 << 10
	// MaxSubscriptionIDBytes caps destination IDs of the mcp type.
	MaxSubscriptionIDBytes = 128

	// EnvelopeVerification is the type of the verification control envelope.
	EnvelopeVerification = "verification"
)

var (
	// ErrInvalidEventData: the event data is not one JSON value.
	ErrInvalidEventData = errors.New("mcpevents: event data is not valid JSON")
	// ErrEnvelopeTooLarge: the envelope would exceed MaxEnvelopeBytes.
	ErrEnvelopeTooLarge = errors.New("mcpevents: envelope exceeds the size limit")
)

// ValidSubscriptionID reports whether id can be a subscription ID, which goes
// out as the X-MCP-Subscription-Id header: 1 to MaxSubscriptionIDBytes bytes
// of [A-Za-z0-9_-].
func ValidSubscriptionID(id string) bool {
	if id == "" || len(id) > MaxSubscriptionIDBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// EventEnvelope serializes the MCP event body
// {"eventId","name","timestamp","data","cursor":null}: eventId is the event
// ID, name the topic, timestamp the event time in RFC 3339 UTC (nanosecond
// precision when present), data the event data spliced in unchanged (bytes,
// key order and number spelling preserved; empty data is null). Every string
// is JSON-encoded, so an event ID or topic can't inject members. The result
// is what gets signed and sent.
func EventEnvelope(event *models.Event) ([]byte, error) {
	data := []byte(event.Data)
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("null")
	}
	// Size first: no point scanning data that can't be sent.
	if len(data) > MaxEnvelopeBytes {
		return nil, ErrEnvelopeTooLarge
	}
	if !json.Valid(data) {
		return nil, ErrInvalidEventData
	}
	b := make([]byte, 0, len(data)+len(event.ID)+len(event.Topic)+112)
	b = append(b, `{"eventId":`...)
	b = appendJSString(b, event.ID)
	b = append(b, `,"name":`...)
	b = appendJSString(b, event.Topic)
	b = append(b, `,"timestamp":`...)
	b = appendJSString(b, event.Time.UTC().Format(time.RFC3339Nano))
	b = append(b, `,"data":`...)
	b = append(b, data...)
	b = append(b, `,"cursor":null}`...)
	if len(b) > MaxEnvelopeBytes {
		return nil, ErrEnvelopeTooLarge
	}
	return b, nil
}

// VerificationEnvelope serializes {"type":"verification","challenge":...}.
func VerificationEnvelope(challenge string) []byte {
	b := make([]byte, 0, 48+len(challenge))
	b = append(b, `{"type":"verification","challenge":`...)
	b = appendJSString(b, challenge)
	return append(b, '}')
}

// MessageID returns a random control-envelope webhook-id,
// "msg_<type>_<24 hex>".
func MessageID(envelopeType string) string {
	var r [12]byte
	_, _ = rand.Read(r[:]) // never fails
	return "msg_" + envelopeType + "_" + hex.EncodeToString(r[:])
}

// DeliveryMessageID returns the webhook-id of an event delivered to a
// destination: "msg_" + 22 base64url characters of
// sha256(tenantID NUL destinationID NUL eventID), unkeyed.
//   - The tenant is an input because destination IDs can be client-supplied
//     and are only unique per tenant; the destination is an input so one event
//     matched by several subscriptions gets a different ID on each.
//   - It is server-generated and high-entropy: a digest, never the event ID.
//   - It uses only ASCII letters, digits, "_" and "-", so it is header-safe.
//   - It has no key because the subscription secret rotates, and a keyed ID
//     would change mid-retry. It is the same on every retry and redelivery
//     without stored state.
func DeliveryMessageID(tenantID, destinationID, eventID string) string {
	sum := sha256.Sum256([]byte(tenantID + "\x00" + destinationID + "\x00" + eventID))
	return "msg_" + base64.RawURLEncoding.EncodeToString(sum[:])[:22]
}

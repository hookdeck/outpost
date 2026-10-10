package mcpevents

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

const (
	// MaxEnvelopeBytes is the largest body the mcp destination sends; larger
	// events fail as payload_too_large without a request.
	MaxEnvelopeBytes = 256 << 10
	// MaxEventIDBytes caps event IDs, which go out as webhook-id.
	MaxEventIDBytes = 256

	// Control envelope types.
	EnvelopeVerification = "verification"
	EnvelopeTerminated   = "terminated"
)

var (
	// ErrInvalidEventID: the event ID can't be a webhook-id header value
	// (visible ASCII, 1..MaxEventIDBytes). Not retryable.
	ErrInvalidEventID = errors.New("mcpevents: event id must be 1-256 visible ASCII characters")
	// ErrInvalidEventData: the event data is not one JSON value.
	ErrInvalidEventData = errors.New("mcpevents: event data is not valid JSON")
	// ErrEnvelopeTooLarge: the envelope would exceed MaxEnvelopeBytes.
	ErrEnvelopeTooLarge = errors.New("mcpevents: envelope exceeds the size limit")
)

// ValidEventID reports whether id can be sent as webhook-id: 1 to
// MaxEventIDBytes bytes of visible ASCII (0x21-0x7E), so never CR, LF, space
// or a quote-breaking control character.
func ValidEventID(id string) bool {
	return visibleASCII(id, MaxEventIDBytes)
}

// visibleASCII reports whether s is 1 to max bytes of 0x21-0x7E, safe as a
// header value.
func visibleASCII(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
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
	if !ValidEventID(event.ID) {
		return nil, ErrInvalidEventID
	}
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

// TerminatedEnvelope serializes {"type":"terminated","error":{code,message,data}}
// for e, with codes from e's profile.
func TerminatedEnvelope(e *Error) ([]byte, error) {
	return marshalNoEscape(struct {
		Type  string   `json:"type"`
		Error RPCError `json:"error"`
	}{EnvelopeTerminated, e.RPCError()})
}

// MessageID returns a random control-envelope webhook-id,
// "msg_<type>_<24 hex>".
func MessageID(envelopeType string) string {
	var r [12]byte
	_, _ = rand.Read(r[:]) // never fails
	return "msg_" + envelopeType + "_" + hex.EncodeToString(r[:])
}

// TerminatedMessageID returns the deterministic webhook-id of a
// subscription's terminated envelope, "msg_terminated_" + the first 24 hex
// characters of sha256(subscriptionID + created_at in Unix ms), so a
// re-sent termination (another pod, a repeated reconciliation pass)
// deduplicates at the receiver while a recreated subscription gets a new ID.
func TerminatedMessageID(subscriptionID string, createdAtMs int64) string {
	sum := sha256.Sum256([]byte(subscriptionID + strconv.FormatInt(createdAtMs, 10)))
	return "msg_" + EnvelopeTerminated + "_" + hex.EncodeToString(sum[:12])
}

// marshalNoEscape is json.Marshal without HTML escaping.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

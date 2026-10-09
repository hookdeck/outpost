package mcpevents

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventEnvelope_ByteExact(t *testing.T) {
	t.Parallel()
	event := &models.Event{
		ID:    "evt_01J9ZK",
		Topic: "order.created",
		Time:  time.Date(2026, 10, 9, 18, 58, 12, 0, time.FixedZone("CEST", 2*3600)),
		// Key order, spacing, number spelling and HTML characters survive.
		Data: json.RawMessage(`{"orderId":"ord_42", "total":180.50,"currency":"USD","note":"<b>&amp;</b>","sep":"` + " " + `","a":1e2}`),
	}
	got, err := EventEnvelope(event)
	require.NoError(t, err)
	assert.Equal(t,
		`{"eventId":"evt_01J9ZK","name":"order.created","timestamp":"2026-10-09T16:58:12Z","data":{"orderId":"ord_42", "total":180.50,"currency":"USD","note":"<b>&amp;</b>","sep":"`+" "+`","a":1e2},"cursor":null}`,
		string(got))
	assert.True(t, json.Valid(got))

	event.Time = time.Date(2026, 10, 9, 16, 58, 12, 123456000, time.UTC)
	got, err = EventEnvelope(event)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"timestamp":"2026-10-09T16:58:12.123456Z"`)
}

func TestEventEnvelope_StringsAreEncoded(t *testing.T) {
	t.Parallel()
	// Visible ASCII, so a valid webhook-id, but full of JSON syntax.
	id := `x","name":"other.topic","data":{"forged":true},"x":"`
	topic := `t<script>"&'` + " "
	got, err := EventEnvelope(&models.Event{ID: id, Topic: topic, Time: time.Unix(0, 0), Data: json.RawMessage(`{"a":1}`)})
	require.NoError(t, err)

	dec := json.NewDecoder(bytes.NewReader(got))
	dec.DisallowUnknownFields()
	var env struct {
		EventID   string          `json:"eventId"`
		Name      string          `json:"name"`
		Timestamp string          `json:"timestamp"`
		Data      json.RawMessage `json:"data"`
		Cursor    *string         `json:"cursor"`
	}
	require.NoError(t, dec.Decode(&env))
	assert.Equal(t, id, env.EventID)
	assert.Equal(t, topic, env.Name)
	assert.Equal(t, `{"a":1}`, string(env.Data))
	assert.Nil(t, env.Cursor)
	assert.Equal(t, 1, strings.Count(string(got), `"name":`))
	assert.Contains(t, string(got), `t<script>\"&'`, "no HTML escaping")
}

func TestEventEnvelope_Errors(t *testing.T) {
	t.Parallel()
	base := func() *models.Event {
		return &models.Event{ID: "evt_1", Topic: "t", Time: time.Unix(0, 0), Data: json.RawMessage(`{}`)}
	}
	for _, id := range []string{"", "evt 1", "evt_1\r\nX-Injected: 1", "evt\x00", "evt\t1", "évt", "evt\x7f", strings.Repeat("a", MaxEventIDBytes+1)} {
		e := base()
		e.ID = id
		_, err := EventEnvelope(e)
		assert.ErrorIs(t, err, ErrInvalidEventID, "%q", id)
		assert.False(t, ValidEventID(id))
	}
	assert.True(t, ValidEventID(strings.Repeat("~", MaxEventIDBytes)))
	assert.True(t, ValidEventID(`!"#$%&'()*+,-./:;<=>?@[\]^_{|}~`))

	for _, data := range []string{`{"a":1},"name":"x"`, `{"a":`, `{} {}`, `nope`} {
		e := base()
		e.Data = json.RawMessage(data)
		_, err := EventEnvelope(e)
		assert.ErrorIs(t, err, ErrInvalidEventData, "%q", data)
	}

	for _, data := range []string{"", "  \n"} {
		e := base()
		e.Data = json.RawMessage(data)
		got, err := EventEnvelope(e)
		require.NoError(t, err)
		assert.Contains(t, string(got), `"data":null,`)
	}

	// Size limit applies to the whole envelope.
	e := base()
	overhead := len(`{"eventId":"evt_1","name":"t","timestamp":"1970-01-01T00:00:00Z","data":,"cursor":null}`)
	e.Data = json.RawMessage(`"` + strings.Repeat("x", MaxEnvelopeBytes-overhead-2) + `"`)
	got, err := EventEnvelope(e)
	require.NoError(t, err)
	assert.Len(t, got, MaxEnvelopeBytes)
	e.Data = json.RawMessage(`"` + strings.Repeat("x", MaxEnvelopeBytes-overhead-1) + `"`)
	_, err = EventEnvelope(e)
	assert.ErrorIs(t, err, ErrEnvelopeTooLarge)
	e.Data = json.RawMessage(strings.Repeat("x", 4*MaxEnvelopeBytes)) // never scanned
	_, err = EventEnvelope(e)
	assert.ErrorIs(t, err, ErrEnvelopeTooLarge)
}

func TestControlEnvelopes(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `{"type":"verification","challenge":"abc-_"}`, string(VerificationEnvelope("abc-_")))

	got, err := TerminatedEnvelope(AccessRevoked())
	require.NoError(t, err)
	assert.Equal(t, `{"type":"terminated","error":{"code":-32012,"message":"Forbidden","data":{"reason":"access_revoked"}}}`, string(got))

	got, err = TerminatedEnvelope(AccessRevoked().WithProfile(CodeProfileSEP3415))
	require.NoError(t, err)
	assert.Equal(t, `{"type":"terminated","error":{"code":-32024,"message":"Forbidden","data":{"reason":"access_revoked"}}}`, string(got))

	got, err = TerminatedEnvelope(SchemaChanged())
	require.NoError(t, err)
	assert.Equal(t, `{"type":"terminated","error":{"code":-32014,"message":"Unsupported","data":{"feature":"payloadSchema","reason":"schema_changed"}}}`, string(got))

	got, err = TerminatedEnvelope(EventEnded().WithProfile(CodeProfileSEP3415))
	require.NoError(t, err)
	assert.Equal(t, `{"type":"terminated","error":{"code":-32023,"message":"NotFound","data":{"kind":"event"}}}`, string(got))
}

func TestMessageIDs(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`^msg_verification_[0-9a-f]{24}$`)
	a, b := MessageID(EnvelopeVerification), MessageID(EnvelopeVerification)
	assert.Regexp(t, re, a)
	assert.NotEqual(t, a, b)
	assert.True(t, ValidEventID(a))

	sum := sha256.Sum256([]byte("sub_3f1c8e2b0d49f7e6a1b2c3d4e5f60718" + "1791576000123"))
	want := "msg_terminated_" + hex.EncodeToString(sum[:])[:24]
	assert.Equal(t, want, TerminatedMessageID("sub_3f1c8e2b0d49f7e6a1b2c3d4e5f60718", 1791576000123))
	assert.Equal(t, want, TerminatedMessageID("sub_3f1c8e2b0d49f7e6a1b2c3d4e5f60718", 1791576000123), "deterministic")
	assert.NotEqual(t, want, TerminatedMessageID("sub_3f1c8e2b0d49f7e6a1b2c3d4e5f60718", 1791576000124), "a recreated subscription gets a new id")
	assert.Regexp(t, `^msg_terminated_[0-9a-f]{24}$`, want)
}

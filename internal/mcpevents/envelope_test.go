package mcpevents

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
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
	// Full of JSON syntax.
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
}

func TestMessageIDs(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`^msg_verification_[0-9a-f]{24}$`)
	a, b := MessageID(EnvelopeVerification), MessageID(EnvelopeVerification)
	assert.Regexp(t, re, a)
	assert.NotEqual(t, a, b)
}

func TestDeliveryMessageID(t *testing.T) {
	t.Parallel()
	id := DeliveryMessageID("t1", "sub_1", "evt_1")
	assert.Regexp(t, `^msg_[A-Za-z0-9_-]{22}$`, id)
	assert.Equal(t, id, DeliveryMessageID("t1", "sub_1", "evt_1"), "stable across attempts")
	assert.NotEqual(t, id, DeliveryMessageID("t2", "sub_1", "evt_1"), "per tenant")
	assert.NotEqual(t, id, DeliveryMessageID("t1", "sub_2", "evt_1"), "per destination")
	assert.NotEqual(t, id, DeliveryMessageID("t1", "sub_1", "evt_2"), "per event")
	assert.NotContains(t, id, "evt_1")

	// The separator keeps (a, bc) and (ab, c) apart.
	assert.NotEqual(t, DeliveryMessageID("t", "a", "bc"), DeliveryMessageID("t", "ab", "c"))
	assert.NotEqual(t, DeliveryMessageID("a", "bc", "e"), DeliveryMessageID("ab", "c", "e"))

	sum := sha256.Sum256([]byte("t1\x00sub_1\x00evt_1"))
	assert.Equal(t, "msg_"+base64.RawURLEncoding.EncodeToString(sum[:])[:22], id)

	// Any event ID, including ones that aren't header-safe, yields a header-safe ID.
	assert.Regexp(t, `^msg_[A-Za-z0-9_-]{22}$`, DeliveryMessageID("t1", "sub_1", "evt 1\r\nX: y\x00é"))
}

func TestValidSubscriptionID(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"sub_1", "A-b_9", strings.Repeat("a", MaxSubscriptionIDBytes)} {
		assert.True(t, ValidSubscriptionID(id), id)
	}
	for _, id := range []string{"", "sub 1", "sub.1", "sub\r\n", "évt", strings.Repeat("a", MaxSubscriptionIDBytes+1)} {
		assert.False(t, ValidSubscriptionID(id), "%q", id)
	}
}

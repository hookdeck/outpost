package mcpreceiver

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newSecret(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return "whsec_" + base64.StdEncoding.EncodeToString(key)
}

func newReceiver(t *testing.T) *Receiver {
	t.Helper()
	r, err := New()
	if err != nil {
		t.Skip(err)
	}
	t.Cleanup(r.Close)
	return r
}

type sent struct {
	status int
	body   string
}

func post(t *testing.T, url, secret, msgID string, ts time.Time, body any, extra map[string]string) sent {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	require.NoError(t, err)
	stamp := strconv.FormatInt(ts.Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", msgID)
	req.Header.Set("webhook-timestamp", stamp)
	req.Header.Set("webhook-signature", "v1,AAAA "+Sign(secret, msgID, stamp, raw))
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return sent{resp.StatusCode, string(b)}
}

func violated(r *Receiver, check string) bool {
	for _, v := range r.Violations() {
		if v.Check == check {
			return true
		}
	}
	return false
}

func event(id string) map[string]any {
	return map[string]any{"eventId": id, "name": "order.created", "timestamp": "2026-10-27T12:00:00Z", "data": map[string]any{"a": 1}, "cursor": nil}
}

const goodMsgID = "msg_abcdefghijklmnopqrstuv"

func TestGoodDeliveryPasses(t *testing.T) {
	r := newReceiver(t)
	secret := newSecret(t)
	p, url := r.Register(PathConfig{Secrets: []string{secret}, SubscriptionID: "dst_1"})
	h := map[string]string{"X-MCP-Subscription-Id": "dst_1"}

	got := post(t, url, secret, "msg_verification_x", time.Now(), map[string]any{"type": "verification", "challenge": "c1"}, h)
	require.Equal(t, 200, got.status)
	require.JSONEq(t, `{"challenge":"c1"}`, got.body)
	require.Equal(t, 200, post(t, url, secret, goodMsgID, time.Now(), event("evt_1"), h).status)
	require.Equal(t, 200, post(t, url, secret, goodMsgID, time.Now().Add(time.Second), event("evt_1"), h).status)

	require.Len(t, r.Challenges(p), 1)
	require.Len(t, r.Events(p), 2)
	require.Empty(t, r.Violations())
}

func TestBadDeliveryFails(t *testing.T) {
	cases := map[string]struct {
		check string
		do    func(t *testing.T, url, secret string)
	}{
		"wrong secret": {CheckSignatureFormula, func(t *testing.T, url, _ string) {
			post(t, url, newSecret(t), goodMsgID, time.Now(), event("e"), map[string]string{"X-MCP-Subscription-Id": "dst_1"})
		}},
		"stale timestamp": {CheckSignatureFormula, func(t *testing.T, url, s string) {
			post(t, url, s, goodMsgID, time.Now().Add(-10*time.Minute), event("e"), map[string]string{"X-MCP-Subscription-Id": "dst_1"})
		}},
		"wrong subscription": {CheckSubscriptionID, func(t *testing.T, url, s string) {
			post(t, url, s, goodMsgID, time.Now(), event("e"), map[string]string{"X-MCP-Subscription-Id": "other"})
		}},
		"webhook-id format": {CheckWebhookIDFormat, func(t *testing.T, url, s string) {
			post(t, url, s, "msg_short", time.Now(), event("e"), map[string]string{"X-MCP-Subscription-Id": "dst_1"})
		}},
		"webhook-id is event id": {CheckWebhookIDFormat, func(t *testing.T, url, s string) {
			post(t, url, s, goodMsgID, time.Now(), event(goodMsgID), map[string]string{"X-MCP-Subscription-Id": "dst_1"})
		}},
		"event with type": {CheckTypeDiscrim, func(t *testing.T, url, s string) {
			e := event("e")
			e["type"] = "event"
			post(t, url, s, goodMsgID, time.Now(), e, map[string]string{"X-MCP-Subscription-Id": "dst_1"})
		}},
		"bad timestamp field": {CheckEnvelopeShape, func(t *testing.T, url, s string) {
			e := event("e")
			e["timestamp"] = "yesterday"
			post(t, url, s, goodMsgID, time.Now(), e, map[string]string{"X-MCP-Subscription-Id": "dst_1"})
		}},
		"no challenge first": {CheckVerifyFirst, func(t *testing.T, url, s string) {
			post(t, url, s, goodMsgID, time.Now(), event("e"), map[string]string{"X-MCP-Subscription-Id": "dst_1"})
		}},
		"retry reuses signature": {CheckRetryResigns, func(t *testing.T, url, s string) {
			raw, _ := json.Marshal(event("e"))
			ts := time.Now().Add(-2 * time.Second).Unix()
			sig := Sign(s, goodMsgID, strconv.FormatInt(ts, 10), raw)
			for _, stamp := range []int64{ts, ts + 1} {
				req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("webhook-id", goodMsgID)
				req.Header.Set("webhook-timestamp", strconv.FormatInt(stamp, 10))
				req.Header.Set("webhook-signature", sig)
				resp, err := http.DefaultClient.Do(req)
				require.NoError(t, err)
				resp.Body.Close()
			}
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := newReceiver(t)
			secret := newSecret(t)
			_, url := r.Register(PathConfig{Secrets: []string{secret}, SubscriptionID: "dst_1"})
			tc.do(t, url, secret)
			require.True(t, violated(r, tc.check))
		})
	}
}

func TestWebhookIDAcrossMessages(t *testing.T) {
	r := newReceiver(t)
	secret := newSecret(t)
	_, url := r.Register(PathConfig{Secrets: []string{secret}})
	post(t, url, secret, "msg_verification_x", time.Now(), map[string]any{"type": "verification", "challenge": "c"}, nil)
	post(t, url, secret, goodMsgID, time.Now(), event("e1"), nil)
	post(t, url, secret, goodMsgID, time.Now(), event("e2"), nil)
	require.True(t, violated(r, CheckWebhookIDPerMsg))

	r2 := newReceiver(t)
	_, url2 := r2.Register(PathConfig{Secrets: []string{secret}})
	post(t, url2, secret, "msg_verification_x", time.Now(), map[string]any{"type": "verification", "challenge": "c"}, nil)
	post(t, url2, secret, goodMsgID, time.Now(), event("e1"), nil)
	post(t, url2, secret, "msg_abcdefghijklmnopqrstuw", time.Now(), event("e1"), nil)
	require.True(t, violated(r2, CheckWebhookIDPerMsg))
}

func TestMultipleSignaturesAndRotation(t *testing.T) {
	r := newReceiver(t)
	oldSecret, newSec := newSecret(t), newSecret(t)
	raw, _ := json.Marshal(event("e"))
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, _ := http.NewRequest(http.MethodPost, r.BaseURL()+"/x", bytes.NewReader(raw))
	req.Header.Set("webhook-id", goodMsgID)
	req.Header.Set("webhook-timestamp", stamp)
	req.Header.Set("webhook-signature", Sign(oldSecret, goodMsgID, stamp, raw)+" "+Sign(newSec, goodMsgID, stamp, raw))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	got := r.Requests("/x")
	require.Len(t, got, 1)
	require.Equal(t, []bool{true, true, false}, got[0].VerifiedWith(oldSecret, newSec, newSecret(t)))
}

func TestBehaviors(t *testing.T) {
	r := newReceiver(t)
	secret := newSecret(t)
	chal := map[string]any{"type": "verification", "challenge": "abc"}
	h := map[string]string{}

	_, u := r.Register(PathConfig{Challenge: WrongEcho()})
	require.JSONEq(t, `{"challenge":"not-abc"}`, post(t, u, secret, "msg_verification_1", time.Now(), chal, h).body)

	_, u = r.Register(PathConfig{Challenge: Status(500), Event: Status(410)})
	require.Equal(t, 500, post(t, u, secret, "msg_verification_1", time.Now(), chal, h).status)
	require.Equal(t, 410, post(t, u, secret, goodMsgID, time.Now(), event("e"), h).status)

	_, u = r.Register(PathConfig{Event: Sequence(500, 503)})
	var got []int
	for range 4 {
		got = append(got, post(t, u, secret, goodMsgID, time.Now(), event("e"), h).status)
	}
	require.Equal(t, []int{500, 503, 200, 200}, got)

	_, u = r.Register(PathConfig{Event: Sleep(200 * time.Millisecond)})
	start := time.Now()
	require.Equal(t, 200, post(t, u, secret, goodMsgID, time.Now(), event("e"), h).status)
	require.GreaterOrEqual(t, time.Since(start), 200*time.Millisecond)

	_, u = r.Register(PathConfig{Challenge: Redirect("/elsewhere")})
	require.Equal(t, 307, post(t, u, secret, "msg_verification_1", time.Now(), chal, h).status)

	_, u = r.Register(PathConfig{Event: Marker()})
	require.Equal(t, MarkerText, post(t, u, secret, goodMsgID, time.Now(), event("e"), h).body)
}

func TestSentinelsCountAccepts(t *testing.T) {
	r := newReceiver(t)
	require.NotZero(t, r.SentinelV4Port())
	require.Zero(t, r.SentinelAccepts())
	c, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(r.SentinelV4Port()))
	require.NoError(t, err)
	c.Close()
	require.Eventually(t, func() bool { return r.SentinelAccepts() == 1 }, time.Second, 10*time.Millisecond)
}

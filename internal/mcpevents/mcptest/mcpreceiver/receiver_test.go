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
		"data is a string": {CheckEnvelopeShape, func(t *testing.T, url, s string) {
			e := event("e")
			e["data"] = "x"
			post(t, url, s, goodMsgID, time.Now(), e, map[string]string{"X-MCP-Subscription-Id": "dst_1"})
		}},
		"data is null": {CheckEnvelopeShape, func(t *testing.T, url, s string) {
			e := event("e")
			e["data"] = nil
			post(t, url, s, goodMsgID, time.Now(), e, map[string]string{"X-MCP-Subscription-Id": "dst_1"})
		}},
		"data is an array": {CheckEnvelopeShape, func(t *testing.T, url, s string) {
			e := event("e")
			e["data"] = []int{1}
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

func listen(t *testing.T, o Options) *Receiver {
	t.Helper()
	r, err := Listen(o)
	require.NoError(t, err)
	t.Cleanup(r.Close)
	return r
}

func local(r *Receiver, path string) string { return "http://" + r.Addr() + path }

var subHeader = map[string]string{"X-MCP-Subscription-Id": "sub_1"}

func TestListenPublicURLAndFallback(t *testing.T) {
	r := listen(t, Options{PublicURL: "http://host.docker.internal:4501/"})
	_, url := r.Register(PathConfig{})
	require.Contains(t, url, "http://host.docker.internal:4501/mcp/")
	require.Zero(t, r.SentinelV4Port())
	require.Zero(t, r.SentinelAccepts())

	secret := newSecret(t)
	var seen []Request
	r = listen(t, Options{
		Profile:   ProfileSpec,
		Fallback:  &PathConfig{Secrets: []string{secret}, Event: Status(202)},
		OnRequest: func(q Request) { seen = append(seen, q) },
	})
	got := post(t, local(r, "/hook"), secret, "msg_verification_x", time.Now(), map[string]any{"type": "verification", "challenge": "c"}, subHeader)
	require.JSONEq(t, `{"challenge":"c"}`, got.body)
	require.Equal(t, 202, post(t, local(r, "/hook"), secret, goodMsgID, time.Now(), event("e"), subHeader).status)
	require.Empty(t, r.Violations())
	require.Len(t, seen, 2)
	require.Equal(t, "verification", seen[0].Kind())
	require.Equal(t, "event", seen[1].Kind())
	require.Len(t, r.AllRequests(), 2)

	post(t, local(r, "/hook"), newSecret(t), goodMsgID, time.Now(), event("e2"), subHeader)
	require.True(t, violated(r, CheckSignatureFormula))
}

func TestRejectUnverified(t *testing.T) {
	r := listen(t, Options{})
	secret := newSecret(t)
	_, url := r.Register(PathConfig{Secrets: []string{secret}, RejectUnverified: true})
	chal := map[string]any{"type": "verification", "challenge": "c"}

	got := post(t, url, newSecret(t), "msg_verification_x", time.Now(), chal, subHeader)
	require.Equal(t, 401, got.status)
	require.Empty(t, got.body)
	require.Equal(t, 401, post(t, url, secret, goodMsgID, time.Now().Add(-10*time.Minute), event("e"), subHeader).status)
	require.Equal(t, 200, post(t, url, secret, "msg_verification_x", time.Now(), chal, subHeader).status)
	require.Equal(t, 200, post(t, url, secret, goodMsgID, time.Now(), event("e"), subHeader).status)
}

func TestFailFirstPerMessage(t *testing.T) {
	r := listen(t, Options{})
	secret := newSecret(t)
	_, url := r.Register(PathConfig{FailFirst: 1})
	second := "msg_abcdefghijklmnopqrstuw"
	var got []int
	for _, id := range []string{goodMsgID, goodMsgID, second, second, goodMsgID} {
		got = append(got, post(t, url, secret, id, time.Now(), event("e"), nil).status)
	}
	require.Equal(t, []int{500, 200, 500, 200, 200}, got)

	_, url = r.Register(PathConfig{FailFirst: 2, FailStatus: 503})
	got = nil
	for range 3 {
		got = append(got, post(t, url, secret, goodMsgID, time.Now(), event("e"), nil).status)
	}
	require.Equal(t, []int{503, 503, 200}, got)

	_, url = r.Register(PathConfig{FailFirst: 1})
	require.Equal(t, 200, post(t, url, secret, "msg_verification_x", time.Now(), map[string]any{"type": "verification", "challenge": "c"}, nil).status)
}

func TestControlEnvelopes(t *testing.T) {
	cases := map[string]struct {
		id    string
		body  map[string]any
		check string
	}{
		"good gap":              {"msg_gap_abc", map[string]any{"type": "gap", "cursor": "c9"}, ""},
		"gap without cursor":    {"msg_gap_abc", map[string]any{"type": "gap"}, CheckEnvelopeShape},
		"gap with null cursor":  {"msg_gap_abc", map[string]any{"type": "gap", "cursor": nil}, CheckEnvelopeShape},
		"gap id prefix":         {goodMsgID, map[string]any{"type": "gap", "cursor": "c9"}, CheckWebhookIDFormat},
		"good terminated":       {"msg_terminated_abc", map[string]any{"type": "terminated", "error": map[string]any{"code": -32024, "message": "Forbidden"}}, ""},
		"terminated no error":   {"msg_terminated_abc", map[string]any{"type": "terminated"}, CheckEnvelopeShape},
		"terminated no code":    {"msg_terminated_abc", map[string]any{"type": "terminated", "error": map[string]any{"message": "x"}}, CheckEnvelopeShape},
		"terminated str code":   {"msg_terminated_abc", map[string]any{"type": "terminated", "error": map[string]any{"code": "1", "message": "x"}}, CheckEnvelopeShape},
		"unknown type is fine":  {"msg_ping_abc", map[string]any{"type": "ping"}, ""},
		"unknown type id check": {"msg_abc", map[string]any{"type": "ping"}, CheckWebhookIDFormat},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := listen(t, Options{Profile: ProfileSpec})
			secret := newSecret(t)
			_, url := r.Register(PathConfig{Secrets: []string{secret}})
			post(t, url, secret, tc.id, time.Now(), tc.body, subHeader)
			if tc.check == "" {
				require.Empty(t, r.Violations())
			} else {
				require.True(t, violated(r, tc.check), "%v", r.Violations())
			}
		})
	}

	t.Run("unsigned control", func(t *testing.T) {
		r := listen(t, Options{})
		_, url := r.Register(PathConfig{Secrets: []string{newSecret(t)}})
		post(t, url, newSecret(t), "msg_gap_abc", time.Now(), map[string]any{"type": "gap", "cursor": "c"}, subHeader)
		require.True(t, violated(r, CheckControlSigned))
	})
}

func TestSubscriptionIDHeaderRequired(t *testing.T) {
	r := listen(t, Options{})
	secret := newSecret(t)
	_, url := r.Register(PathConfig{Secrets: []string{secret}})
	post(t, url, secret, goodMsgID, time.Now(), event("e"), nil)
	require.True(t, violated(r, CheckHeaders))
}

func TestProfiles(t *testing.T) {
	longID := "msg_abcdefghijklmnopqrstuvwxyz0123"
	secret := newSecret(t)
	run := func(p Profile, first string, id string, ev map[string]any) *Receiver {
		r := listen(t, Options{Profile: p})
		_, url := r.Register(PathConfig{Secrets: []string{secret}})
		if first == "event" {
			post(t, url, secret, id, time.Now(), ev, subHeader)
		} else {
			post(t, url, secret, "msg_verification_x", time.Now(), map[string]any{"type": "verification", "challenge": "c"}, subHeader)
			post(t, url, secret, id, time.Now(), ev, subHeader)
		}
		return r
	}
	withCursor := func(c any) map[string]any { e := event("e"); e["cursor"] = c; return e }

	spec := run(ProfileSpec, "event", longID, withCursor("c1"))
	require.Empty(t, spec.Violations())
	spec = run(ProfileSpec, "challenge", "plain-id_1", withCursor(nil))
	require.Empty(t, spec.Violations())
	spec = run(ProfileSpec, "challenge", "bad id", withCursor(nil))
	require.True(t, violated(spec, CheckWebhookIDFormat))
	spec = run(ProfileSpec, "challenge", "same", map[string]any{"eventId": "same", "name": "n", "timestamp": "2026-10-27T12:00:00Z", "data": 1})
	require.True(t, violated(spec, CheckWebhookIDFormat))
	spec = run(ProfileSpec, "challenge", goodMsgID, withCursor(5))
	require.True(t, violated(spec, CheckEnvelopeShape))

	out := run(ProfileOutpost, "event", longID, withCursor("c1"))
	require.True(t, violated(out, CheckWebhookIDFormat))
	require.True(t, violated(out, CheckEnvelopeShape))
	require.True(t, violated(out, CheckVerifyFirst))
}

func TestRejectedRequestsDoNotConsumeScriptedFailures(t *testing.T) {
	r := listen(t, Options{})
	secret := newSecret(t)
	_, url := r.Register(PathConfig{Secrets: []string{secret}, RejectUnverified: true, FailFirst: 1})
	require.Equal(t, 401, post(t, url, newSecret(t), goodMsgID, time.Now(), event("e"), subHeader).status)
	require.Equal(t, 500, post(t, url, secret, goodMsgID, time.Now(), event("e"), subHeader).status)
	require.Equal(t, 200, post(t, url, secret, goodMsgID, time.Now(), event("e"), subHeader).status)

	_, url = r.Register(PathConfig{Secrets: []string{secret}, RejectUnverified: true, Event: Sequence(503)})
	require.Equal(t, 401, post(t, url, newSecret(t), goodMsgID, time.Now(), event("e"), subHeader).status)
	require.Equal(t, 503, post(t, url, secret, goodMsgID, time.Now(), event("e"), subHeader).status)
}

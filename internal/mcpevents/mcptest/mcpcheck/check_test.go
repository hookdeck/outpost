package mcpcheck

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type faults struct {
	ignoreName       bool
	wrongSecret      bool
	ignoreArgs       bool
	newIDOnRetry     bool
	idIgnoresCaller  bool
	allowHTTP        bool
	wrongData        bool
	noChallenge      bool
	noRetryOnFailure bool
}

type sub struct {
	id, principal, name, url string
	args                     map[string]any
	secrets                  []string
}

// fakeServer is a small conforming MCP Events server with webhook delivery.
type fakeServer struct {
	*httptest.Server
	f      faults
	mu     sync.Mutex
	subs   map[string]*sub
	nextEv atomic.Int64
	wg     sync.WaitGroup
}

func newFake(t *testing.T, f faults) *fakeServer {
	s := &fakeServer{f: f, subs: map[string]*sub{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(func() { s.wg.Wait(); s.Close() })
	return s
}

func rpc(w http.ResponseWriter, status int, id json.RawMessage, result any, code int, data map[string]any) {
	m := map[string]any{"jsonrpc": "2.0", "id": id}
	if code != 0 {
		e := map[string]any{"code": code, "message": "error"}
		if data != nil {
			e["data"] = data
		}
		m["error"] = e
	} else {
		m["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(m)
}

const goodSecretLen = 32

func validSecret(s string) bool {
	raw, ok := strings.CutPrefix(s, "whsec_")
	if !ok {
		return false
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	return err == nil && len(b) == goodSecretLen
}

func (s *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
			Delivery  struct {
				URL    string `json:"url"`
				Secret string `json:"secret"`
			} `json:"delivery"`
		} `json:"params"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Method == "server/discover" {
		rpc(w, 200, req.ID, map[string]any{"resultType": "complete", "supportedVersions": []string{"2026-07-28"},
			"capabilities": map[string]any{"extensions": map[string]any{"io.modelcontextprotocol/events": map[string]any{}}}}, 0, nil)
		return
	}
	principal := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if principal == "" {
		rpc(w, 200, req.ID, nil, -32024, nil)
		return
	}
	switch req.Method {
	case "events/list":
		rpc(w, 200, req.ID, map[string]any{"resultType": "complete", "events": []any{map[string]any{
			"name": "order.created", "delivery": []string{"webhook"},
			"inputSchema":   map[string]any{"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string", "enum": []string{"paid", "pending"}}}},
			"payloadSchema": map[string]any{"type": "object", "examples": []any{map[string]any{"id": "o1"}}},
		}}}, 0, nil)
	case "events/subscribe", "events/unsubscribe":
		if !s.f.ignoreName && (r.Header.Get("Mcp-Name") != req.Params.Name || req.Params.Name == "") {
			rpc(w, 400, req.ID, nil, -32020, nil)
			return
		}
		if req.Params.Name != "order.created" {
			rpc(w, 200, req.ID, nil, -32023, map[string]any{"kind": "event"})
			return
		}
		s.subscription(w, r, req.ID, req.Method == "events/subscribe", principal, req.Params.Name, req.Params.Arguments, req.Params.Delivery.URL, req.Params.Delivery.Secret)
	default:
		rpc(w, 200, req.ID, nil, -32601, nil)
	}
}

func (s *fakeServer) key(principal, name string, args map[string]any, u string) string {
	if s.f.idIgnoresCaller {
		principal, args = "", nil
	}
	b, _ := json.Marshal([]any{principal, name, args, u})
	sum := sha256.Sum256(b)
	return "sub_" + hex.EncodeToString(sum[:8])
}

func (s *fakeServer) subscription(w http.ResponseWriter, r *http.Request, id json.RawMessage, subscribe bool, principal, name string, args map[string]any, u, secret string) {
	key := s.key(principal, name, args, u)
	s.mu.Lock()
	existing := s.subs[key]
	s.mu.Unlock()
	if !subscribe {
		if existing == nil {
			rpc(w, 200, id, nil, -32023, nil)
			return
		}
		s.mu.Lock()
		delete(s.subs, key)
		s.mu.Unlock()
		rpc(w, 200, id, map[string]any{"resultType": "complete"}, 0, nil)
		return
	}
	parsed, err := url.Parse(u)
	if err != nil || !validSecret(secret) {
		rpc(w, 200, id, nil, -32602, nil)
		return
	}
	if parsed.Scheme == "http" && parsed.Hostname() != "127.0.0.1" {
		if !s.f.allowHTTP {
			rpc(w, 200, id, nil, -32602, nil)
		} else {
			rpc(w, 200, id, map[string]any{"resultType": "complete", "id": key, "refreshBefore": time.Now().Add(time.Hour).Format(time.RFC3339)}, 0, nil)
		}
		return
	}
	n := &sub{id: key, principal: principal, name: name, url: u, args: args, secrets: []string{secret}}
	if existing != nil && existing.secrets[0] != secret {
		n.secrets = append(n.secrets, existing.secrets[0])
	}
	if existing == nil && !s.f.noChallenge {
		if !s.challenge(n) {
			rpc(w, 200, id, nil, -32027, map[string]any{"reason": "challenge_failed"})
			return
		}
	}
	s.mu.Lock()
	s.subs[key] = n
	s.mu.Unlock()
	rpc(w, 200, id, map[string]any{
		"resultType": "complete", "id": key, "refreshBefore": time.Now().Add(10 * time.Minute).Format(time.RFC3339),
		"truncated": false, "cursor": nil,
	}, 0, nil)
}

func randID(n int) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

func (s *fakeServer) post(sb *sub, msgID string, ts int64, body []byte, secrets []string) (int, []byte) {
	req, _ := http.NewRequest(http.MethodPost, sb.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", msgID)
	req.Header.Set("webhook-timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-MCP-Subscription-Id", sb.id)
	var sigs []string
	for _, sec := range secrets {
		if s.f.wrongSecret {
			sec = "whsec_" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
		}
		sigs = append(sigs, mcpreceiver.Sign(sec, msgID, strconv.FormatInt(ts, 10), body))
	}
	req.Header.Set("webhook-signature", strings.Join(sigs, " "))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (s *fakeServer) challenge(sb *sub) bool {
	nonce := randID(16)
	body, _ := json.Marshal(map[string]any{"type": "verification", "challenge": nonce})
	status, out := s.post(sb, "msg_verification_"+randID(16), time.Now().Unix(), body, sb.secrets)
	var echo struct {
		Challenge string `json:"challenge"`
	}
	_ = json.Unmarshal(out, &echo)
	return status == 200 && echo.Challenge == nonce
}

// publish delivers an event to the matching subscriptions and returns its ID.
func (s *fakeServer) publish(name string, data map[string]any) string {
	evID := fmt.Sprintf("evt_%d", s.nextEv.Add(1))
	s.mu.Lock()
	var targets []*sub
	for _, sb := range s.subs {
		match := sb.name == name
		for k, v := range sb.args {
			if !s.f.ignoreArgs && data[k] != v {
				match = false
			}
		}
		if match {
			c := *sb
			targets = append(targets, &c)
		}
	}
	s.mu.Unlock()
	if s.f.wrongData {
		data = map[string]any{"unexpected": true}
	}
	body, _ := json.Marshal(map[string]any{"eventId": evID, "name": name, "timestamp": time.Now().Format(time.RFC3339), "data": data, "cursor": nil})
	for _, sb := range targets {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			msgID := "msg_" + randID(22)
			for attempt := 0; attempt < 3; attempt++ {
				if attempt > 0 && s.f.newIDOnRetry {
					msgID = "msg_" + randID(22)
				}
				status, _ := s.post(sb, msgID, time.Now().Unix()+int64(attempt), body, sb.secrets)
				if status/100 == 2 || s.f.noRetryOnFailure {
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
		}()
	}
	return evID
}

func runCheck(t *testing.T, s *fakeServer, mutate func(*Options)) Report {
	t.Helper()
	rcv, err := mcpreceiver.Listen(mcpreceiver.Options{Addr: "127.0.0.1:0", Profile: mcpreceiver.ProfileSpec})
	require.NoError(t, err)
	t.Cleanup(rcv.Close)
	o := Options{
		ServerURL: s.URL, Principal: "alice", Receiver: rcv,
		Publish: func(_ context.Context, ev string, data map[string]any) (string, error) {
			return s.publish(ev, data), nil
		},
		Wait: 5 * time.Second, Quiet: 300 * time.Millisecond, HTTPClient: &http.Client{Timeout: 20 * time.Second},
	}
	if mutate != nil {
		mutate(&o)
	}
	return Run(context.Background(), o)
}

func status(rep Report, id string) Result {
	for _, r := range rep {
		if r.ID == id {
			return r
		}
	}
	return Result{}
}

// The receiver is reached over http, which a conforming development
// server accepts with a warning.
func assertAllPassButHTTP(t *testing.T, rep Report) {
	t.Helper()
	for _, r := range rep {
		if r.ID == "subscribe.https-required" {
			assert.Equal(t, Warn, r.Status)
			assert.Equal(t, httpAccepted, r.Detail)
			continue
		}
		assert.Equal(t, Pass, r.Status, "%s: %s", r.ID, r.Detail)
	}
}

func TestConformingServerPassesEverything(t *testing.T) {
	rep := runCheck(t, newFake(t, faults{}), nil)
	assertAllPassButHTTP(t, rep)
	assert.False(t, rep.Failed())
	assert.Greater(t, len(rep), 35)
}

func TestFaultsFailTheirChecks(t *testing.T) {
	for name, c := range map[string]struct {
		f    faults
		want []string
	}{
		"ignores Mcp-Name":    {faults{ignoreName: true}, []string{"headers.name-missing"}},
		"wrong signing key":   {faults{wrongSecret: true}, []string{"delivery.signature"}},
		"ignores arguments":   {faults{ignoreArgs: true}, []string{"filter.no-match"}},
		"new webhook-id":      {faults{newIDOnRetry: true}, []string{"delivery.retry"}},
		"id ignores caller":   {faults{idIgnoresCaller: true}, []string{"subscribe.isolation"}},
		"echoes wrong data":   {faults{wrongData: true}, []string{"delivery.data-matches"}},
		"no challenge at all": {faults{noChallenge: true}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rep := runCheck(t, newFake(t, c.f), nil)
			for _, id := range c.want {
				assert.Equal(t, Fail, status(rep, id).Status, "%s: %s", id, status(rep, id).Detail)
			}
			if len(c.want) > 0 {
				assert.True(t, rep.Failed())
			}
		})
	}
}

func TestMissingChallengeWarns(t *testing.T) {
	rep := runCheck(t, newFake(t, faults{noChallenge: true}), nil)
	assert.Equal(t, Warn, status(rep, "subscribe.verification").Status)
	assert.Equal(t, Skip, status(rep, "delivery.control-signed").Status)
	assert.Equal(t, Warn, status(rep, "subscribe.isolation").Status)
}

func TestHTTPCallbacksWarn(t *testing.T) {
	rep := runCheck(t, newFake(t, faults{allowHTTP: true}), nil)
	r := status(rep, "subscribe.https-required")
	assert.Equal(t, Warn, r.Status)
	assert.Equal(t, httpAccepted, r.Detail)
	assert.False(t, rep.Failed())
}

func TestNoRetrySkips(t *testing.T) {
	rep := runCheck(t, newFake(t, faults{noRetryOnFailure: true}), func(o *Options) { o.Wait = 600 * time.Millisecond })
	assert.Equal(t, Skip, status(rep, "delivery.retry").Status)
	assert.Equal(t, Skip, status(rep, "delivery.retry-resigns").Status)
}

func TestWaitModeSkipsPublishChecks(t *testing.T) {
	s := newFake(t, faults{})
	go func() {
		time.Sleep(1500 * time.Millisecond)
		s.publish("order.created", map[string]any{"id": "o1"})
	}()
	rep := runCheck(t, s, func(o *Options) { o.Publish = nil; o.Wait = 10 * time.Second })
	assert.False(t, rep.Failed(), rep.String())
	for _, id := range publishOnly {
		r := status(rep, id)
		assert.Equal(t, Skip, r.Status, id)
		assert.Equal(t, publishOnlyDetail, r.Detail)
	}
	assert.Equal(t, Pass, status(rep, "delivery.received").Status)
	assert.Equal(t, Skip, status(rep, "delivery.retry-resigns").Status)
	assert.Contains(t, rep.String(), "8 checks need publish mode")
}

func TestExplicitArgumentsAndData(t *testing.T) {
	rep := runCheck(t, newFake(t, faults{}), func(o *Options) {
		o.Event = "order.created"
		o.Arguments = map[string]any{"status": "pending"}
		o.Data = map[string]any{"id": "x", "status": "pending"}
		o.OtherData = map[string]any{"id": "y", "status": "paid"}
	})
	assertAllPassButHTTP(t, rep)
}

func TestUnknownEventFails(t *testing.T) {
	rep := runCheck(t, newFake(t, faults{}), func(o *Options) { o.Event = "nope" })
	assert.Equal(t, Fail, status(rep, "list.event").Status)
	assert.Equal(t, Skip, status(rep, "subscribe.result").Status)
}

func TestReportString(t *testing.T) {
	rep := Report{{ID: "a", Status: Pass}, {ID: "bb", Status: Skip, Detail: publishOnlyDetail}}
	s := rep.String()
	assert.Contains(t, s, "1 passed, 0 failed, 0 warnings, 1 skipped")
	assert.Contains(t, s, "1 checks need publish mode")
	assert.True(t, reflect.DeepEqual(rep.Count(Pass), 1))
}

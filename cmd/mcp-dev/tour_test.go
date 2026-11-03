package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSub struct {
	id, url, secret string
	args            map[string]any
}

// fakeOutpost implements the routes the tour uses.
type fakeOutpost struct {
	*httptest.Server
	deliverAll bool
	mu         sync.Mutex
	subs       []fakeSub
	events     int
	wg         sync.WaitGroup
}

func newFakeOutpost(t *testing.T, deliverAll bool) *fakeOutpost {
	f := &fakeOutpost{deliverAll: deliverAll}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() { f.wg.Wait(); f.Close() })
	return f
}

func (f *fakeOutpost) post(url, secret, id string, body []byte, sub string) []byte {
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", id)
	req.Header.Set("webhook-timestamp", ts)
	req.Header.Set("webhook-signature", mcpreceiver.Sign(secret, id, ts, body))
	req.Header.Set("X-MCP-Subscription-Id", sub)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return out
}

func (f *fakeOutpost) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/mcp/events":
		_, _ = io.WriteString(w, `{"resultType":"complete","events":[{"name":"incident.created","delivery":["webhook"],
			"inputSchema":{"type":"object","properties":{"severity":{"type":"string","enum":["P1","P2","P3"]}}},
			"payloadSchema":{"type":"object","examples":[{"id":"inc_1"}]}}]}`)
	case strings.HasSuffix(r.URL.Path, "/mcp/subscribe"):
		var req struct {
			Params struct {
				Arguments map[string]any `json:"arguments"`
				Delivery  struct{ URL, Secret string }
			} `json:"params"`
		}
		_ = json.Unmarshal(raw, &req)
		s := fakeSub{id: "sub_1", url: req.Params.Delivery.URL, secret: req.Params.Delivery.Secret, args: req.Params.Arguments}
		f.mu.Lock()
		known := len(f.subs) > 0
		f.mu.Unlock()
		if !known {
			ch, _ := json.Marshal(map[string]any{"type": "verification", "challenge": "c1"})
			f.post(s.url, s.secret, "msg_verification_x", ch, s.id)
			f.mu.Lock()
			f.subs = append(f.subs, s)
			f.mu.Unlock()
		}
		_, _ = io.WriteString(w, `{"resultType":"complete","id":"sub_1","refreshBefore":"2026-10-31T15:14:05Z","truncated":false}`)
	case strings.HasSuffix(r.URL.Path, "/mcp/unsubscribe"):
		f.mu.Lock()
		f.subs = nil
		f.mu.Unlock()
		_, _ = io.WriteString(w, `{"resultType":"complete"}`)
	case r.URL.Path == "/publish":
		var req struct {
			Topic string         `json:"topic"`
			Data  map[string]any `json:"data"`
		}
		_ = json.Unmarshal(raw, &req)
		f.mu.Lock()
		f.events++
		id := fmt.Sprintf("evt_%d", f.events)
		subs := append([]fakeSub(nil), f.subs...)
		f.mu.Unlock()
		body, _ := json.Marshal(map[string]any{"eventId": id, "name": req.Topic, "timestamp": time.Now().Format(time.RFC3339), "data": req.Data})
		for _, s := range subs {
			if f.deliverAll || req.Data["severity"] == s.args["severity"] {
				f.wg.Add(1)
				go func() {
					defer f.wg.Done()
					f.post(s.url, s.secret, "msg_abcdefghijklmnopqrstuv", body, s.id)
				}()
			}
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"id":%q}`, id)
	default:
		http.NotFound(w, r)
	}
}

func runFakeTour(t *testing.T, deliverAll bool) (bool, string, error) {
	f := newFakeOutpost(t, deliverAll)
	var out bytes.Buffer
	ok, err := runTour(context.Background(), tourConfig{
		conn:   conn{URL: f.URL, APIKey: "k"},
		tenant: "demo", principal: "alice",
		listen: "127.0.0.1:0",
		wait:   3 * time.Second, quiet: 300 * time.Millisecond,
		out: &out, in: strings.NewReader(""),
	})
	return ok, out.String(), err
}

func TestTourPasses(t *testing.T) {
	ok, out, err := runFakeTour(t, false)
	require.NoError(t, err)
	assert.True(t, ok, out)
	assert.Contains(t, out, "Tour complete: 6/6 ok")
	assert.Contains(t, out, "challenge answered before subscribe returned")
	assert.Contains(t, out, "severity=P1 delivered with a valid signature; severity=P2 not delivered")
	assert.NotContains(t, out, "FAIL")
}

func TestTourFailsWhenNonMatchingEventArrives(t *testing.T) {
	ok, out, err := runFakeTour(t, true)
	require.NoError(t, err)
	assert.False(t, ok, out)
	assert.Contains(t, out, "FAIL: the non-matching event (severity=P2) was delivered")
	assert.NotContains(t, out, "Tour complete")
}

func TestTourSetupHintWhenOutpostIsDown(t *testing.T) {
	var out bytes.Buffer
	ok, err := runTour(context.Background(), tourConfig{
		conn: conn{URL: "http://127.0.0.1:1"}, tenant: "demo", principal: "alice", listen: "127.0.0.1:0",
		wait: time.Second, quiet: time.Millisecond, out: &out, in: strings.NewReader(""),
	})
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, out.String(), "make up/mcp")
}

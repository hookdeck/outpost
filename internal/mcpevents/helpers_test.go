package mcpevents

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
)

// fakeClock is a settable clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	// Mid-minute, so a test doesn't straddle a rate window by accident.
	return &fakeClock{t: time.Now().Truncate(time.Minute).Add(30 * time.Second)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// guardedLikeClient mirrors the production client's redirect and proxy
// behaviour (without the address guard, so httptest servers are reachable).
func guardedLikeClient(t *testing.T) *http.Client {
	t.Helper()
	tr := &http.Transport{Proxy: nil, MaxResponseHeaderBytes: 64 << 10}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func newMiniredis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return mr, client
}

// receivedRequest is one request a receiver got.
type receivedRequest struct {
	Header    http.Header
	Body      []byte
	Challenge string
	Verified  bool // signature valid for the receiver's key
}

// receiver is an MCP Events webhook endpoint for tests: it records requests,
// checks Standard Webhooks signatures with its key, and answers with respond.
type receiver struct {
	*httptest.Server
	key     []byte
	respond func(w http.ResponseWriter, r receivedRequest)

	mu       sync.Mutex
	requests []receivedRequest
}

func newReceiver(t *testing.T, key []byte, respond func(w http.ResponseWriter, r receivedRequest)) *receiver {
	t.Helper()
	rc := &receiver{key: key, respond: respond}
	rc.Server = httptest.NewServer(http.HandlerFunc(rc.serve))
	t.Cleanup(rc.Close)
	return rc
}

func (rc *receiver) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	req := receivedRequest{Header: r.Header.Clone(), Body: body}
	if wh, err := standardwebhooks.NewWebhookRaw(rc.key); err == nil {
		req.Verified = wh.VerifyIgnoringTimestamp(body, r.Header) == nil
	}
	var env struct {
		Challenge string `json:"challenge"`
	}
	_ = json.Unmarshal(body, &env)
	req.Challenge = env.Challenge
	rc.mu.Lock()
	rc.requests = append(rc.requests, req)
	rc.mu.Unlock()
	if rc.respond != nil {
		rc.respond(w, req)
	}
}

func (rc *receiver) Requests() []receivedRequest {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]receivedRequest(nil), rc.requests...)
}

// echo answers a verification challenge correctly, after checking the
// signature as a real receiver must.
func echo(w http.ResponseWriter, r receivedRequest) {
	if !r.Verified {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"challenge":"`+r.Challenge+`"}`)
}

// leakCheck fails the test if goroutines started during it outlive it. Call
// it first (cleanups run last-in first-out) and don't combine with
// t.Parallel.
func leakCheck(t *testing.T) {
	t.Helper()
	before := runtime.NumGoroutine()
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if after := runtime.NumGoroutine(); after > before {
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			t.Errorf("goroutine leak: %d before, %d after\n%s", before, after, buf[:n])
		}
	})
}

// countingLimiter is a HostLimiter with a fixed number of slots per host.
type countingLimiter struct {
	mu       sync.Mutex
	max      int
	inFlight map[string]int
	acquired int
	released int
	denied   int
}

func newCountingLimiter(max int) *countingLimiter {
	return &countingLimiter{max: max, inFlight: map[string]int{}}
}

func (l *countingLimiter) TryAcquire(hostport string) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight[hostport] >= l.max {
		l.denied++
		return nil, false
	}
	l.inFlight[hostport]++
	l.acquired++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.inFlight[hostport]--
			l.released++
		})
	}, true
}

func (l *countingLimiter) counts() (acquired, released, denied int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.acquired, l.released, l.denied
}

func hostOf(rawURL string) string {
	return strings.TrimPrefix(strings.TrimPrefix(rawURL, "http://"), "https://")
}

// rawRequest is one request as it came over the wire: header names keep
// their spelling, which an http.Server would canonicalize.
type rawRequest struct {
	headerNames []string
	header      http.Header
	body        []byte
}

// deliveryHeaderNames returns the names of r's delivery headers (those
// SetHeaders writes, matched case-insensitively) as spelled on the wire.
func (r rawRequest) deliveryHeaderNames() []string {
	var names []string
	for _, name := range r.headerNames {
		switch strings.ToLower(name) {
		case "content-type", "webhook-id", "webhook-timestamp", "webhook-signature", "x-mcp-subscription-id":
			names = append(names, name)
		}
	}
	return names
}

// newRawReceiver serves HTTP/1.1 on 127.0.0.1 by hand, so the test sees the
// header names as the client wrote them. respond returns the body of the 200
// answering each request. It returns the base URL and the requests.
func newRawReceiver(t *testing.T, respond func(body []byte) string) (string, <-chan rawRequest) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := make(chan rawRequest, 16)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				reader := textproto.NewReader(bufio.NewReader(conn))
				if _, err := reader.ReadLine(); err != nil {
					return
				}
				req := rawRequest{header: http.Header{}}
				for {
					line, err := reader.ReadLine()
					if err != nil || line == "" {
						break
					}
					name, value, _ := strings.Cut(line, ":")
					req.headerNames = append(req.headerNames, name)
					req.header.Add(name, strings.TrimSpace(value))
				}
				n, _ := strconv.Atoi(req.header.Get("Content-Length"))
				req.body = make([]byte, n)
				_, _ = io.ReadFull(reader.R, req.body)
				answer := ""
				if respond != nil {
					answer = respond(req.body)
				}
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: "+
					strconv.Itoa(len(answer))+"\r\nConnection: close\r\n\r\n"+answer)
				got <- req
			}()
		}
	}()
	return "http://" + ln.Addr().String(), got
}

// receiveRaw returns the next request of a raw receiver.
func receiveRaw(t *testing.T, got <-chan rawRequest) rawRequest {
	t.Helper()
	select {
	case req := <-got:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("no request received")
		return rawRequest{}
	}
}

// canonicalHeader returns h with canonical keys, the form http.Header.Get
// and Standard Webhooks libraries read: SetHeaders writes the documented
// spellings, which only a request parsed off the wire canonicalizes.
func canonicalHeader(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for name, values := range h {
		for _, v := range values {
			out.Add(name, v)
		}
	}
	return out
}

// sortedLike orders delivery header names as SetHeaders documents them, so
// a test compares spellings regardless of the order they were written in.
func sortedLike(names []string) []string {
	order := map[string]int{"content-type": 0, "webhook-id": 1, "webhook-timestamp": 2, "webhook-signature": 3, "x-mcp-subscription-id": 4}
	out := slices.Clone(names)
	slices.SortStableFunc(out, func(a, b string) int {
		return order[strings.ToLower(a)] - order[strings.ToLower(b)]
	})
	return out
}

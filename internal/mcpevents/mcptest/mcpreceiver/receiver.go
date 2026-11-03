// Package mcpreceiver is a reference receiver for MCP Events webhook
// deliveries. It answers verification challenges and deliveries as scripted
// per path, records every request raw, and checks them against the
// delivery rules.
package mcpreceiver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// BindAddr is where the receiver listens: a loopback address that is not
// 127.0.0.1, so allowlisting it leaves 127.0.0.1 and ::1 (the sentinels)
// blocked.
const BindAddr = "127.0.0.2:0"

// ErrCannotBind is returned by New when BindAddr is not available.
var ErrCannotBind = errors.New("cannot bind 127.0.0.2")

type behaviorKind int

const (
	kindDefault behaviorKind = iota
	kindWrongEcho
	kindStatus
	kindSequence
	kindSleep
	kindRedirect
	kindMarker
)

// Behavior is how a path answers. The zero value echoes challenges and
// answers deliveries 200.
type Behavior struct {
	kind   behaviorKind
	status int
	seq    []int
	delay  time.Duration
	target string
}

// Echo answers challenges correctly and deliveries 200 (the default).
func Echo() Behavior { return Behavior{} }

// WrongEcho answers a challenge with a different challenge.
func WrongEcho() Behavior { return Behavior{kind: kindWrongEcho} }

// Status answers every request with status n.
func Status(n int) Behavior { return Behavior{kind: kindStatus, status: n} }

// Sequence answers with the given statuses in order, then 200.
func Sequence(statuses ...int) Behavior { return Behavior{kind: kindSequence, seq: statuses} }

// Sleep holds the answer for d, then answers as the default does.
func Sleep(d time.Duration) Behavior { return Behavior{kind: kindSleep, delay: d} }

// Redirect answers 307 to target (a path on this receiver or a URL).
func Redirect(target string) Behavior { return Behavior{kind: kindRedirect, target: target} }

// Marker answers 200 with a body that must never surface in an Outpost
// response; see MarkerText.
func Marker() Behavior { return Behavior{kind: kindMarker} }

// MarkerText is the body a Marker path answers with.
const MarkerText = "mcpreceiver-marker-7f3a9c1e5b2d4086"

// PathConfig describes one callback path.
type PathConfig struct {
	// Secrets sign-check the requests; any one matching v1 signature passes.
	Secrets []string
	// SubscriptionID is the expected X-MCP-Subscription-Id ("" = unchecked).
	SubscriptionID string
	// Challenge and Event answer verification challenges and event
	// deliveries.
	Challenge Behavior
	Event     Behavior
	// Unchecked keeps the path out of the delivery-rule checks (a path used
	// by a non-MCP destination).
	Unchecked bool
}

// Request is one request the receiver got.
type Request struct {
	Path     string
	Method   string
	Header   http.Header
	Body     []byte
	Received time.Time
}

// Envelope is the decoded request body.
type Envelope struct {
	Type      string          `json:"type"`
	Challenge string          `json:"challenge"`
	EventID   string          `json:"eventId"`
	Name      string          `json:"name"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
	Cursor    json.RawMessage `json:"cursor"`
}

// Envelope decodes the body; a body that isn't a JSON object yields the
// zero Envelope.
func (r Request) Envelope() Envelope {
	var e Envelope
	_ = json.Unmarshal(r.Body, &e)
	return e
}

func (r Request) IsChallenge() bool { return r.Envelope().Type == "verification" }
func (r Request) IsEvent() bool     { return r.Envelope().EventID != "" }

type path struct {
	cfg  PathConfig
	seq  []int
	init bool
}

// Receiver is the HTTP receiver plus its sentinel listeners.
type Receiver struct {
	ln       net.Listener
	server   *http.Server
	sentinel []*sentinel

	mu       sync.Mutex
	paths    map[string]*path
	requests []Request
	nextID   atomic.Int64
}

type sentinel struct {
	ln      net.Listener
	accepts atomic.Int64
}

// New starts a receiver on BindAddr and the sentinel listeners on
// 127.0.0.1 and [::1]. A sentinel that can't bind is left out (its port
// reads 0).
func New() (*Receiver, error) {
	ln, err := net.Listen("tcp4", BindAddr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCannotBind, err)
	}
	r := &Receiver{ln: ln, paths: map[string]*path{}}
	for _, addr := range []string{"tcp4:127.0.0.1:0", "tcp6:[::1]:0"} {
		network, address := addr[:4], addr[5:]
		s := &sentinel{}
		if sl, err := net.Listen(network, address); err == nil {
			s.ln = sl
			go s.run()
		}
		r.sentinel = append(r.sentinel, s)
	}
	r.server = &http.Server{Handler: http.HandlerFunc(r.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = r.server.Serve(ln) }()
	return r, nil
}

func (s *sentinel) run() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.accepts.Add(1)
		c.Close()
	}
}

// Close stops the receiver and the sentinels.
func (r *Receiver) Close() {
	_ = r.server.Close()
	for _, s := range r.sentinel {
		if s.ln != nil {
			s.ln.Close()
		}
	}
}

// Addr is host:port of the receiver.
func (r *Receiver) Addr() string { return r.ln.Addr().String() }

// BaseURL is http://host:port.
func (r *Receiver) BaseURL() string { return "http://" + r.Addr() }

func (r *Receiver) sentinelPort(i int) int {
	if r.sentinel[i].ln == nil {
		return 0
	}
	return r.sentinel[i].ln.Addr().(*net.TCPAddr).Port
}

// SentinelV4Port is the port of the 127.0.0.1 sentinel (0 = not bound).
func (r *Receiver) SentinelV4Port() int { return r.sentinelPort(0) }

// SentinelV6Port is the port of the [::1] sentinel (0 = not bound).
func (r *Receiver) SentinelV6Port() int { return r.sentinelPort(1) }

// SentinelAccepts is the number of TCP connections the sentinels accepted.
func (r *Receiver) SentinelAccepts() int {
	var n int64
	for _, s := range r.sentinel {
		n += s.accepts.Load()
	}
	return int(n)
}

// Register adds a callback path with a fresh unique name and returns the
// path and its URL.
func (r *Receiver) Register(cfg PathConfig) (string, string) {
	p := "/mcp/" + strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatInt(r.nextID.Add(1), 10)
	r.Set(p, cfg)
	return p, r.BaseURL() + p
}

// Set (re)configures a path.
func (r *Receiver) Set(p string, cfg PathConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths[p] = &path{cfg: cfg}
}

// SetSecrets replaces the secrets of a path.
func (r *Receiver) SetSecrets(p string, secrets ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pp := r.paths[p]; pp != nil {
		pp.cfg.Secrets = secrets
	}
}

func (r *Receiver) serve(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(io.LimitReader(req.Body, 4<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	rec := Request{Path: req.URL.Path, Method: req.Method, Header: req.Header.Clone(), Body: body, Received: time.Now()}
	env := rec.Envelope()
	challenge := env.Type == "verification"

	r.mu.Lock()
	r.requests = append(r.requests, rec)
	p := r.paths[req.URL.Path]
	var b Behavior
	if p != nil {
		b = p.cfg.Event
		if challenge {
			b = p.cfg.Challenge
		}
		if b.kind == kindSequence && !challenge {
			if !p.init {
				p.seq, p.init = append([]int(nil), b.seq...), true
			}
			if len(p.seq) > 0 {
				b = Behavior{kind: kindStatus, status: p.seq[0]}
				p.seq = p.seq[1:]
			} else {
				b = Behavior{}
			}
		}
	}
	r.mu.Unlock()

	switch b.kind {
	case kindStatus:
		w.WriteHeader(b.status)
		return
	case kindRedirect:
		w.Header().Set("Location", b.target)
		w.WriteHeader(http.StatusTemporaryRedirect)
		return
	case kindSleep:
		time.Sleep(b.delay)
	case kindMarker:
		_, _ = io.WriteString(w, MarkerText)
		return
	}
	if challenge {
		c := env.Challenge
		if b.kind == kindWrongEcho {
			c = "not-" + c
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": c})
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Requests returns the requests to path, in arrival order. It also works
// for paths that were never registered.
func (r *Receiver) Requests(path string) []Request {
	return r.Matching(path, nil)
}

// Matching returns the requests to path that match (nil matches all).
func (r *Receiver) Matching(path string, match func(Request) bool) []Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Request
	for _, q := range r.requests {
		if q.Path == path && (match == nil || match(q)) {
			out = append(out, q)
		}
	}
	return out
}

// Challenges returns the verification challenges received on path.
func (r *Receiver) Challenges(path string) []Request {
	return r.Matching(path, Request.IsChallenge)
}

// Events returns the event deliveries received on path.
func (r *Receiver) Events(path string) []Request {
	return r.Matching(path, Request.IsEvent)
}

// WaitFor polls until path has at least n matching requests and returns
// them, or returns what it has at the timeout.
func (r *Receiver) WaitFor(path string, n int, match func(Request) bool, timeout time.Duration) []Request {
	deadline := time.Now().Add(timeout)
	for {
		got := r.Matching(path, match)
		if len(got) >= n || time.Now().After(deadline) {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
}

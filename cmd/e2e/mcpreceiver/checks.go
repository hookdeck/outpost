package mcpreceiver

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Violation is a delivery rule that a recorded request broke.
type Violation struct {
	Check  string
	Detail string
}

// Checks run by the receiver over the recorded requests.
const (
	CheckPostJSON         = "delivery is a JSON POST"
	CheckHeaders          = "Standard Webhooks headers present"
	CheckSubscriptionID   = "X-MCP-Subscription-Id header matches"
	CheckSignatureFormula = "event signature verifies within the timestamp window"
	CheckWebhookIDFormat  = "webhook-id format"
	CheckWebhookIDPerMsg  = "webhook-id is stable per message and unique across messages"
	CheckRetryResigns     = "retries carry a fresh timestamp and signature"
	CheckEnvelopeShape    = "event envelope shape"
	CheckTypeDiscrim      = "only verification envelopes carry a type"
	CheckControlSigned    = "verification requests are signed like events"
	CheckVerifyFirst      = "verification precedes the first event"
)

var (
	headerSafe     = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	eventMessageID = regexp.MustCompile(`^msg_[A-Za-z0-9_-]{22}$`)
)

type checkResult struct {
	fail string
}

func (g *checkResult) bad(format string, args ...any) {
	if g.fail == "" {
		g.fail = fmt.Sprintf(format, args...)
	}
}

// Violations checks every recorded request against the delivery rules and
// returns the rules that were broken.
func (r *Receiver) Violations() []Violation {
	r.mu.Lock()
	reqs := append([]Request(nil), r.requests...)
	paths := make(map[string]PathConfig, len(r.paths))
	for k, v := range r.paths {
		paths[k] = v.cfg
	}
	r.mu.Unlock()

	g := map[string]*checkResult{}
	for _, id := range []string{
		CheckPostJSON, CheckHeaders, CheckSubscriptionID, CheckSignatureFormula,
		CheckWebhookIDFormat, CheckWebhookIDPerMsg, CheckRetryResigns,
		CheckEnvelopeShape, CheckTypeDiscrim, CheckControlSigned, CheckVerifyFirst,
	} {
		g[id] = &checkResult{}
	}

	type msgKey struct{ path, eventID, subscription string }
	byMsg := map[msgKey][]Request{}
	idOwner := map[string]msgKey{}
	firstSeen := map[string]bool{}

	for _, q := range reqs {
		cfg, registered := paths[q.Path]
		if !registered || cfg.Unchecked {
			continue
		}
		env := q.Envelope()
		challenge := env.Type == "verification"
		where := q.Path

		if !(q.Method == "POST" && strings.HasPrefix(strings.ToLower(q.Header.Get("Content-Type")), "application/json")) {
			g[CheckPostJSON].bad("%s: method %s, content-type %q", where, q.Method, q.Header.Get("Content-Type"))
		}

		id, ts := q.Header.Get("webhook-id"), q.Header.Get("webhook-timestamp")
		if id == "" || ts == "" || len(SignatureValues(q.Header)) == 0 {
			g[CheckHeaders].bad("%s: webhook-id=%q webhook-timestamp=%q signatures=%d", where, id, ts, len(SignatureValues(q.Header)))
		}

		if cfg.SubscriptionID != "" {
			if got := q.Header.Get("X-MCP-Subscription-Id"); got != cfg.SubscriptionID {
				g[CheckSubscriptionID].bad("%s: X-MCP-Subscription-Id=%q, want %q", where, got, cfg.SubscriptionID)
			}
		}

		if len(cfg.Secrets) > 0 {
			target := g[CheckSignatureFormula]
			if challenge {
				target = g[CheckControlSigned]
			}
			matched := false
			for _, m := range q.VerifiedWith(cfg.Secrets...) {
				matched = matched || m
			}
			switch {
			case !matched:
				target.bad("%s: no v1 signature matches a configured secret", where)
			case !q.TimestampWithinSkew():
				target.bad("%s: webhook-timestamp %q not within 5 minutes", where, ts)
			}
		}

		if challenge {
			if !strings.HasPrefix(id, "msg_verification_") || !headerSafe.MatchString(id) {
				g[CheckWebhookIDFormat].bad("%s: verification webhook-id %q", where, id)
			}
			if env.Challenge == "" {
				g[CheckTypeDiscrim].bad("%s: verification envelope without challenge", where)
			}
			firstSeen[q.Path] = true
			continue
		}

		if env.EventID == "" {
			g[CheckEnvelopeShape].bad("%s: body is not an event or verification envelope: %.80s", where, q.Body)
			continue
		}
		if !firstSeen[q.Path] {
			firstSeen[q.Path] = true
			g[CheckVerifyFirst].bad("%s: first request is an event, not a challenge", where)
		}

		switch {
		case !eventMessageID.MatchString(id):
			g[CheckWebhookIDFormat].bad("%s: webhook-id %q is not msg_ + 22 url-safe chars", where, id)
		case id == env.EventID:
			g[CheckWebhookIDFormat].bad("%s: webhook-id equals eventId %q", where, id)
		}

		if _, err := time.Parse(time.RFC3339, env.Timestamp); err != nil || env.Name == "" || env.Data == nil {
			g[CheckEnvelopeShape].bad("%s: eventId=%q name=%q timestamp=%q data=%v", where, env.EventID, env.Name, env.Timestamp, env.Data != nil)
		} else if c := strings.TrimSpace(string(env.Cursor)); c != "" && c != "null" {
			g[CheckEnvelopeShape].bad("%s: cursor %s, want null or absent", where, c)
		}
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(q.Body, &raw)
		if _, has := raw["type"]; has {
			g[CheckTypeDiscrim].bad("%s: event envelope has a type field", where)
		}

		k := msgKey{q.Path, env.EventID, q.Header.Get("X-MCP-Subscription-Id")}
		byMsg[k] = append(byMsg[k], q)
		if owner, seen := idOwner[id]; seen && owner != k {
			g[CheckWebhookIDPerMsg].bad("webhook-id %q shared by %v and %v", id, owner, k)
		}
		idOwner[id] = k
	}

	for k, list := range byMsg {
		ids := map[string]bool{}
		for _, q := range list {
			ids[q.Header.Get("webhook-id")] = true
		}
		if len(ids) > 1 {
			g[CheckWebhookIDPerMsg].bad("%v: webhook-id changed across %d requests", k, len(list))
		}
		for i := 1; i < len(list); i++ {
			prev, cur := list[i-1], list[i]
			pts, cts := prev.Header.Get("webhook-timestamp"), cur.Header.Get("webhook-timestamp")
			switch {
			case len(cts) < len(pts) || (len(cts) == len(pts) && cts < pts):
				g[CheckRetryResigns].bad("%v: timestamp went back %s -> %s", k, pts, cts)
			case cts != pts && strings.Join(SignatureValues(cur.Header), " ") == strings.Join(SignatureValues(prev.Header), " "):
				g[CheckRetryResigns].bad("%v: retry %d reused the signature", k, i)
			}
		}
	}

	var out []Violation
	for id, gr := range g {
		if gr.fail != "" {
			out = append(out, Violation{Check: id, Detail: gr.fail})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Check < out[j].Check })
	return out
}

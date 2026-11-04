package mcpcheck

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
)

// Main flow.

func (r *runner) mainSubscribe() bool {
	r.secrets = []string{mcpclient.NewSecret()}
	r.path, r.url = r.rcv.Register(mcpreceiver.PathConfig{Secrets: r.secrets})
	before := time.Now()
	resp, err := r.c.Subscribe(r.ctx, r.params(r.url, r.secrets[0], r.o.Arguments))
	switch {
	case err != nil:
		r.fail("subscribe.result", "events/subscribe: %v", err)
	case resp.Error != nil || resp.Status != http.StatusOK:
		r.fail("subscribe.result", "events/subscribe: http %d %s", resp.Status, snippet(resp.Raw))
	default:
		if probs := validateSubscribeResult(resp.Result); len(probs) > 0 {
			r.fail("subscribe.result", "%s", strings.Join(probs, "; "))
		} else {
			r.pass("subscribe.result", "")
		}
		var res mcpclient.SubscribeResult
		_ = json.Unmarshal(resp.Result, &res)
		r.subID = res.ID
		if strings.HasPrefix(r.url, "http://") && r.results["subscribe.https-required"].Status != Fail {
			delete(r.results, "subscribe.https-required")
			r.warn("subscribe.https-required", "%s", httpAccepted)
		}
		if res.RefreshBefore != nil {
			if t, err := time.Parse(time.RFC3339Nano, *res.RefreshBefore); err == nil {
				if limit := before.Add(r.o.TTL + time.Second); t.After(limit) {
					r.warn("subscribe.ttl-grant", "refreshBefore %s is later than the suggested ttl allows", *res.RefreshBefore)
				} else {
					r.pass("subscribe.ttl-grant", "")
				}
			}
		}
		if r.subID != "" {
			r.rcv.Set(r.path, mcpreceiver.PathConfig{Secrets: r.secrets, SubscriptionID: r.subID})
		}
		if len(r.rcv.Challenges(r.path)) > 0 {
			r.pass("subscribe.verification", "signed challenge answered before subscribe returned")
		} else {
			r.warn("subscribe.verification", "no challenge arrived: the server used another verification path (allowlist, out-of-band, well-known)")
		}
		return r.subID != ""
	}
	r.blocked = "subscribe failed"
	return false
}

// pubRef identifies one published event.
type pubRef struct {
	id     string
	before int
	data   map[string]any
}

func (r *runner) publish(data map[string]any) (pubRef, error) {
	ref := pubRef{before: len(r.rcv.Events(r.path)), data: data}
	id, err := r.o.Publish(r.ctx, r.event.Name, data)
	ref.id = id
	return ref, err
}

func (p pubRef) match(q mcpreceiver.Request) bool {
	return q.IsEvent() && (p.id == "" || q.Envelope().EventID == p.id)
}

// attempts returns the deliveries of the published event on the main path.
func (r *runner) attempts(p pubRef) []mcpreceiver.Request {
	got := r.rcv.Matching(r.path, p.match)
	if p.id == "" {
		all := r.rcv.Events(r.path)
		if len(all) > p.before {
			return all[p.before:]
		}
		return nil
	}
	return got
}

func (r *runner) waitFor(p pubRef, n int, d time.Duration) []mcpreceiver.Request {
	deadline := time.Now().Add(d)
	for {
		got := r.attempts(p)
		if len(got) >= n || time.Now().After(deadline) || r.ctx.Err() != nil {
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (r *runner) deliveries() {
	if !r.publishMode() {
		r.logf("waiting up to %s for a %s delivery: trigger one upstream", r.o.Wait, r.event.Name)
		ref := pubRef{before: len(r.rcv.Events(r.path))}
		got := r.waitFor(ref, 1, r.o.Wait)
		if len(got) == 0 {
			r.fail("delivery.received", "no event within %s", r.o.Wait)
			return
		}
		r.delivered = true
		r.pass("delivery.received", "")
		return
	}
	ref, err := r.publish(r.mainData)
	if err != nil {
		r.fail("delivery.received", "publish: %v", err)
		return
	}
	got := r.waitFor(ref, 1, r.o.Wait)
	if len(got) == 0 {
		r.fail("delivery.received", "no event within %s", r.o.Wait)
		return
	}
	r.delivered = true
	r.pass("delivery.received", "")
	r.dataMatches(ref, got[0])
	r.retry()
}

func (r *runner) logf(format string, args ...any) { r.o.Logf(format, args...) }

func normalize(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func (r *runner) dataMatches(ref pubRef, q mcpreceiver.Request) {
	env := q.Envelope()
	var delivered any
	_ = json.Unmarshal(env.Data, &delivered)
	switch {
	case env.Name != r.event.Name:
		r.fail("delivery.data-matches", "name %q, want %q", env.Name, r.event.Name)
	case !reflect.DeepEqual(normalize(ref.data), delivered):
		r.fail("delivery.data-matches", "data %s differs from the published %s", env.Data, mustJSON(ref.data))
	default:
		r.pass("delivery.data-matches", "")
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (r *runner) retry() {
	id := "delivery.retry"
	r.rcv.Set(r.path, mcpreceiver.PathConfig{Secrets: r.secrets, SubscriptionID: r.subID, Event: mcpreceiver.Sequence(500)})
	defer r.rcv.Set(r.path, mcpreceiver.PathConfig{Secrets: r.secrets, SubscriptionID: r.subID})
	ref, err := r.publish(r.mainData)
	if err != nil {
		r.fail(id, "publish: %v", err)
		return
	}
	got := r.waitFor(ref, 2, r.o.Wait)
	if len(got) < 2 {
		r.skip(id, "no retry within %s after a 500: the server's retry delay may be longer (Outpost: EXPERIMENTAL_MCP_RETRY_SCHEDULE; make up/mcp sets 1,2,5)", r.o.Wait)
		return
	}
	a, b := got[0], got[1]
	ts1, _ := strconv.ParseInt(a.Header.Get("webhook-timestamp"), 10, 64)
	ts2, _ := strconv.ParseInt(b.Header.Get("webhook-timestamp"), 10, 64)
	switch {
	case a.Header.Get("webhook-id") != b.Header.Get("webhook-id"):
		r.fail(id, "webhook-id changed on retry: %q then %q", a.Header.Get("webhook-id"), b.Header.Get("webhook-id"))
	case ts2 < ts1:
		r.fail(id, "webhook-timestamp went back: %d then %d", ts1, ts2)
	case !b.VerifiedWith(r.secrets[0])[0]:
		r.fail(id, "the retry's signature does not verify with the secret")
	case ts2 == ts1:
		r.warn(id, "retry within the same second, can't tell whether it was re-signed")
	case slices.Equal(mcpreceiver.SignatureValues(a.Header), mcpreceiver.SignatureValues(b.Header)):
		r.fail(id, "the retry reused the first attempt's signature")
	default:
		r.pass(id, "same webhook-id, fresh timestamp and signature (%d attempts)", len(got))
	}
}

func (r *runner) receiverRows() {
	paths := []string{r.path}
	if r.filterPath != "" {
		paths = append(paths, r.filterPath)
	}
	violations := map[string]string{}
	for _, v := range r.rcv.ViolationsOn(paths...) {
		if id, ok := receiverChecks[v.Check]; ok {
			violations[id] = v.Detail
		}
	}
	retried, control := false, false
	for _, p := range paths {
		seen := map[string]int{}
		for _, q := range r.rcv.Requests(p) {
			switch q.Kind() {
			case "event":
				key := q.Envelope().EventID + "|" + q.Header.Get("X-MCP-Subscription-Id")
				seen[key]++
				retried = retried || seen[key] > 1
			case "invalid":
			default:
				control = true
			}
		}
	}
	for _, id := range receiverChecks {
		switch {
		case !r.applies(id):
		case !r.delivered && id != "delivery.verify-first" && id != "delivery.control-signed":
			r.skip(id, "no delivery received")
		case violations[id] != "":
			r.fail(id, "%s", violations[id])
		case id == "delivery.retry-resigns" && !retried:
			r.skip(id, "no retry seen")
		case id == "delivery.control-signed" && !control:
			r.skip(id, "no control request seen")
		default:
			r.pass(id, "")
		}
	}

	var names, big []string
	events := 0
	for _, q := range r.rcv.Requests(r.path) {
		if q.IsEvent() {
			events++
			if n := q.Envelope().Name; n != r.event.Name {
				names = append(names, n)
			}
		}
		if len(q.Body) > maxBodyBytes {
			big = append(big, fmt.Sprintf("%d bytes", len(q.Body)))
		}
	}
	switch {
	case events == 0:
		r.skipAll("no delivery received", "delivery.name", "delivery.body-size")
	default:
		if len(names) > 0 {
			r.fail("delivery.name", "envelope name %q, subscribed to %q", names[0], r.event.Name)
		} else {
			r.pass("delivery.name", "")
		}
		if len(big) > 0 {
			r.warn("delivery.body-size", "body of %s exceeds 256 KiB", big[0])
		} else {
			r.pass("delivery.body-size", "")
		}
	}
}

package mcpcheck

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
)

func (r *runner) deriveCases() {
	r.fc, r.fcErr = FilterCase(r.event, r.o.Arguments, r.o.Data, r.o.OtherData)
	r.mainData = r.fc.Data
	if r.mainData == nil {
		r.mainData = map[string]any{}
	}
}

func (r *runner) filterReady() (string, bool) {
	if !r.publishMode() {
		return "", false
	}
	if r.o.Arguments == nil && r.fc.Match == nil {
		return ErrNoArgument.Error(), false
	}
	if r.fc.OtherData == nil {
		return "no non-matching payload: pass --other-data", false
	}
	if r.fc.Other == nil {
		return "no non-matching arguments", false
	}
	return "", true
}

// Filter and isolation phase: its own path and subscriptions, so the main
// flow's state doesn't matter.

type delivery struct {
	req mcpreceiver.Request
	sub string
}

func (r *runner) filterPhase() {
	ids := []string{"filter.match", "filter.no-match", "subscribe.isolation"}
	if !r.publishMode() {
		return
	}
	if why, ok := r.filterReady(); !ok {
		r.skipAll(why, ids...)
		return
	}
	s1, s2 := mcpclient.NewSecret(), mcpclient.NewSecret()
	path, url := r.rcv.Register(mcpreceiver.PathConfig{Secrets: []string{s1, s2}})
	r.filterPath = path
	p1 := r.params(url, s1, r.fc.Match)
	resp1, err := r.c.Subscribe(r.ctx, p1)
	if err != nil || resp1.Error != nil || resp1.Status != http.StatusOK {
		r.skipAll(fmt.Sprintf("subscribe with arguments failed: %v %s", err, snippet(resp1.Raw)), ids...)
		return
	}
	var res1, res2 mcpclient.SubscribeResult
	_ = json.Unmarshal(resp1.Result, &res1)
	defer func() {
		_, _ = r.c.Unsubscribe(r.ctx, mcpclient.UnsubscribeParams{Name: r.event.Name, Arguments: r.fc.Match, URL: url})
		if res2.ID != "" {
			_, _ = r.c2.Unsubscribe(r.ctx, mcpclient.UnsubscribeParams{Name: r.event.Name, Arguments: r.fc.Other, URL: url})
		}
	}()

	secondOK := true
	resp2, err := r.c2.Subscribe(r.ctx, r.params(url, s2, r.fc.Other))
	switch {
	case err != nil:
		r.skip("subscribe.isolation", "second principal: %v", err)
		secondOK = false
	case resp2.Error != nil && resp2.Error.Code == mcpclient.CodeForbidden:
		r.skip("subscribe.isolation", "second principal refused: pass --second-principal")
		secondOK = false
	case resp2.Error != nil || resp2.Status != http.StatusOK:
		r.fail("subscribe.isolation", "second principal's subscribe failed: http %d %s", resp2.Status, snippet(resp2.Raw))
		secondOK = false
	default:
		_ = json.Unmarshal(resp2.Result, &res2)
	}

	hit, err := r.o.Publish(r.ctx, r.event.Name, r.fc.Data)
	miss, err2 := r.o.Publish(r.ctx, r.event.Name, r.fc.OtherData)
	if err != nil || err2 != nil || hit == "" || miss == "" {
		why := "publish hook returned no event id"
		if err != nil || err2 != nil {
			why = fmt.Sprintf("publish: %v %v", err, err2)
		}
		r.skipAll(why, "filter.match", "filter.no-match")
		if secondOK {
			r.skip("subscribe.isolation", "%s", why)
		}
		return
	}

	byEvent := func(id, sub string) []mcpreceiver.Request {
		return r.rcv.Matching(path, func(q mcpreceiver.Request) bool {
			return q.IsEvent() && q.Envelope().EventID == id && q.Header.Get("X-MCP-Subscription-Id") == sub
		})
	}
	deadline := time.Now().Add(r.o.Wait)
	for time.Now().Before(deadline) && r.ctx.Err() == nil {
		if len(byEvent(hit, res1.ID)) > 0 && (!secondOK || len(byEvent(miss, res2.ID)) > 0) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(r.o.Quiet)

	got := byEvent(hit, res1.ID)
	switch {
	case len(got) != 1:
		r.fail("filter.match", "%d deliveries of the matching event to the subscription, want 1", len(got))
	case !got[0].VerifiedWith(s1)[0]:
		r.fail("filter.match", "the delivery does not verify with the subscription's secret")
	default:
		r.pass("filter.match", "%s=%v", r.fc.Field, r.fc.Match[r.fc.Field])
	}
	if n := len(byEvent(miss, res1.ID)); n > 0 {
		r.fail("filter.no-match", "the non-matching event (%s=%v) was delivered %d time(s)", r.fc.Field, r.fc.Other[r.fc.Field], n)
	} else {
		r.pass("filter.no-match", "")
	}
	if secondOK {
		r.isolation(path, res1.ID, res2.ID, hit, miss, s1, s2, byEvent)
	}
}

func (r *runner) isolation(path, id1, id2, hit, miss, s1, s2 string, byEvent func(id, sub string) []mcpreceiver.Request) {
	const id = "subscribe.isolation"
	var probs []string
	if id1 == id2 {
		r.fail(id, "both principals got subscription id %q", id1)
		return
	}
	chal := map[string]int{}
	for _, q := range r.rcv.Challenges(path) {
		chal[q.Header.Get("X-MCP-Subscription-Id")]++
	}
	warn := ""
	switch {
	case chal[id2] == 0 && chal[id1] > 0:
		probs = append(probs, "the second principal's subscribe got no challenge although the first one's did: one principal's verification must not waive another's")
	case chal[id2] == 0:
		warn = "no challenge for either principal: the server verified another way"
	}
	if n := len(byEvent(hit, id2)); n > 0 {
		probs = append(probs, "the second principal received the first one's event")
	}
	got := byEvent(miss, id2)
	switch {
	case len(got) != 1:
		probs = append(probs, fmt.Sprintf("the second principal received %d deliveries of its event, want 1", len(got)))
	default:
		if v := got[0].VerifiedWith(s1, s2); v[0] || !v[1] {
			probs = append(probs, "the second principal's delivery must verify with its own secret only")
		}
	}
	switch {
	case len(probs) > 0:
		r.fail(id, "%s", strings.Join(probs, "; "))
	case warn != "":
		r.warn(id, "%s", warn)
	default:
		r.pass(id, "same URL, different arguments: each subscription got only its own event")
	}
}

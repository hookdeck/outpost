package mcpcheck

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
)

func (r *runner) subscribeAgain(secret string) (*mcpclient.Response, mcpclient.SubscribeResult, error) {
	resp, err := r.c.Subscribe(r.ctx, r.params(r.url, secret, r.o.Arguments))
	var res mcpclient.SubscribeResult
	if err == nil && resp.Error == nil && resp.Status == http.StatusOK {
		_ = json.Unmarshal(resp.Result, &res)
	}
	return resp, res, err
}

func (r *runner) refresh() {
	resp, res, err := r.subscribeAgain(r.secrets[0])
	switch {
	case err != nil:
		r.fail("refresh.same-id", "events/subscribe: %v", err)
		r.fail("refresh.result", "events/subscribe: %v", err)
	case resp.Error != nil || resp.Status != http.StatusOK:
		r.fail("refresh.same-id", "http %d %s", resp.Status, snippet(resp.Raw))
		r.fail("refresh.result", "http %d %s", resp.Status, snippet(resp.Raw))
	default:
		if res.ID != r.subID {
			r.fail("refresh.same-id", "id %q, first subscribe returned %q", res.ID, r.subID)
		} else {
			r.pass("refresh.same-id", "")
		}
		if probs := validateSubscribeResult(resp.Result); len(probs) > 0 {
			r.fail("refresh.result", "%s", strings.Join(probs, "; "))
		} else {
			r.pass("refresh.result", "")
		}
	}
	if r.publishMode() && r.delivered {
		r.rotate()
	} else if r.publishMode() {
		r.skipAll("no delivery received", "refresh.rotate-secret", "refresh.dual-sign")
	}
}

func (r *runner) rotate() {
	next := mcpclient.NewSecret()
	resp, res, err := r.subscribeAgain(next)
	if err != nil || resp.Error != nil || resp.Status != http.StatusOK {
		r.fail("refresh.rotate-secret", "re-subscribe with a new secret failed: %v %s", err, snippet(resp.Raw))
		r.skip("refresh.dual-sign", "rotation failed")
		return
	}
	if res.ID != r.subID {
		r.fail("refresh.rotate-secret", "id changed on rotation: %q then %q", r.subID, res.ID)
		r.skip("refresh.dual-sign", "rotation failed")
		return
	}
	old := r.secrets[0]
	r.secrets = []string{next, old}
	r.rcv.Set(r.path, mcpreceiver.PathConfig{Secrets: r.secrets, SubscriptionID: r.subID})
	ref, err := r.publish(r.mainData)
	if err != nil {
		r.fail("refresh.rotate-secret", "publish: %v", err)
		r.skip("refresh.dual-sign", "publish failed")
		return
	}
	got := r.waitFor(ref, 1, r.o.Wait)
	if len(got) == 0 {
		r.fail("refresh.rotate-secret", "no delivery within %s after rotation", r.o.Wait)
		r.skip("refresh.dual-sign", "no delivery")
		return
	}
	ok := got[0].VerifiedWith(next, old)
	if ok[0] {
		r.pass("refresh.rotate-secret", "")
	} else {
		r.fail("refresh.rotate-secret", "the delivery after rotation does not verify with the new secret")
	}
	if ok[1] {
		r.pass("refresh.dual-sign", "")
	} else {
		r.warn("refresh.dual-sign", "the delivery after rotation is not also signed with the old secret")
	}
}

func (r *runner) unsubscribe() {
	p := mcpclient.UnsubscribeParams{Name: r.event.Name, Arguments: r.o.Arguments, URL: r.url}
	resp, err := r.c.Unsubscribe(r.ctx, p)
	switch {
	case err != nil:
		r.fail("unsubscribe.result", "events/unsubscribe: %v", err)
	case resp.Error != nil || resp.Status != http.StatusOK:
		r.fail("unsubscribe.result", "http %d %s", resp.Status, snippet(resp.Raw))
	default:
		if obj, _ := parseObject(resp.Result); obj.str("resultType") != "complete" {
			r.fail("unsubscribe.result", "resultType %q, want complete", obj.str("resultType"))
		} else {
			r.pass("unsubscribe.result", "")
		}
	}
	if r.publishMode() {
		r.stopsDelivery()
	}
	resp, err = r.c.Unsubscribe(r.ctx, p)
	switch {
	case err != nil:
		r.fail("unsubscribe.not-found", "request: %v", err)
	case resp.Error == nil || resp.Error.Code != mcpclient.CodeNotFound:
		r.fail("unsubscribe.not-found", "want NotFound (-32023), got http %d %s", resp.Status, snippet(resp.Raw))
	default:
		r.pass("unsubscribe.not-found", "")
	}
}

func (r *runner) stopsDelivery() {
	id := "unsubscribe.stops-delivery"
	ref, err := r.publish(r.mainData)
	if err != nil {
		r.fail(id, "publish: %v", err)
		return
	}
	if got := r.waitFor(ref, 1, r.o.Quiet); len(got) > 0 {
		r.fail(id, "event %s was delivered after unsubscribe", got[0].Envelope().EventID)
		return
	}
	r.pass(id, "nothing arrived in %s", r.o.Quiet)
}

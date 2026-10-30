package mcpcheck

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
)

func (r *runner) probePath() (string, string) {
	return r.rcv.Register(mcpreceiver.PathConfig{Unchecked: true})
}

func (r *runner) noChallenge(path string) bool { return len(r.rcv.Challenges(path)) == 0 }

// refused describes how a server refused a request: an RPC error code or
// an HTTP status.
func refused(resp *mcpclient.Response, code int) (string, bool) {
	switch {
	case resp.Error != nil && resp.Error.Code == code:
		return fmt.Sprintf("error %d", code), true
	case resp.Error == nil && (resp.Status == http.StatusUnauthorized || resp.Status == http.StatusForbidden):
		return fmt.Sprintf("HTTP %d from the auth layer", resp.Status), true
	}
	return "", false
}

func (r *runner) probes() {
	name := r.event.Name

	for _, m := range []string{"subscribe", "unsubscribe"} {
		id := "auth." + m
		p, u := r.probePath()
		var resp *mcpclient.Response
		var err error
		if m == "subscribe" {
			resp, err = r.anon.Subscribe(r.ctx, r.params(u, mcpclient.NewSecret(), r.o.Arguments))
		} else {
			resp, err = r.anon.Unsubscribe(r.ctx, mcpclient.UnsubscribeParams{Name: name, Arguments: r.o.Arguments, URL: u})
		}
		switch {
		case err != nil:
			r.fail(id, "request: %v", err)
		case !r.noChallenge(p):
			r.fail(id, "a challenge was sent without credentials")
		default:
			if how, ok := refused(resp, mcpclient.CodeForbidden); ok {
				r.pass(id, "%s", how)
			} else {
				r.fail(id, "want Forbidden (-32024) or HTTP 401/403, got http %d %s", resp.Status, snippet(resp.Raw))
			}
		}
	}

	for _, c := range []struct {
		id   string
		opt  mcpclient.CallOption
		what string
	}{
		{"headers.name-missing", mcpclient.OmitNameHeader(), "no Mcp-Name header"},
		{"headers.name-mismatch", mcpclient.NameHeader("mcp-check.other"), "Mcp-Name differs from params.name"},
	} {
		p, u := r.probePath()
		resp, err := r.c.Subscribe(r.ctx, r.params(u, mcpclient.NewSecret(), r.o.Arguments), c.opt)
		switch {
		case err != nil:
			r.fail(c.id, "request: %v", err)
		case resp.Status != http.StatusBadRequest || resp.Error == nil || resp.Error.Code != mcpclient.CodeHeaderMismatch:
			r.fail(c.id, "%s: want HTTP 400 and HeaderMismatch (-32020), got http %d %s", c.what, resp.Status, snippet(resp.Raw))
		case !r.noChallenge(p):
			r.fail(c.id, "%s: a challenge was sent", c.what)
		default:
			r.pass(c.id, "")
		}
	}

	{
		p, u := r.probePath()
		prm := r.params(u, mcpclient.NewSecret(), nil)
		prm.Name = unknownEvent
		resp, err := r.c.Subscribe(r.ctx, prm)
		switch {
		case err != nil:
			r.fail("subscribe.unknown-event", "request: %v", err)
		case resp.Error == nil || resp.Error.Code != mcpclient.CodeNotFound:
			r.fail("subscribe.unknown-event", "want NotFound (-32023), got http %d %s", resp.Status, snippet(resp.Raw))
		case !r.noChallenge(p):
			r.fail("subscribe.unknown-event", "a challenge was sent")
		default:
			if kind, ok := resp.Error.DataMap()["kind"]; ok && kind != "event" {
				r.fail("subscribe.unknown-event", "data.kind is %v, want \"event\"", kind)
			} else {
				r.pass("subscribe.unknown-event", "")
			}
		}
	}

	{
		var bad []string
		for _, secret := range []string{"not-a-secret", "whsec_AAECAwQFBgcICQoLDA0ODw=="} {
			p, u := r.probePath()
			resp, err := r.c.Subscribe(r.ctx, r.params(u, secret, r.o.Arguments))
			switch {
			case err != nil:
				bad = append(bad, err.Error())
			case resp.Error == nil || resp.Error.Code != mcpclient.CodeInvalidParams:
				bad = append(bad, fmt.Sprintf("secret %q: want InvalidParams (-32602), got http %d %s", secret, resp.Status, snippet(resp.Raw)))
			case !r.noChallenge(p):
				bad = append(bad, fmt.Sprintf("secret %q: a challenge was sent", secret))
			}
		}
		if len(bad) > 0 {
			r.fail("subscribe.invalid-secret", "%s", strings.Join(bad, "; "))
		} else {
			r.pass("subscribe.invalid-secret", "")
		}
	}

	r.httpsProbe()

	{
		_, u := r.rcv.Register(mcpreceiver.PathConfig{Unchecked: true, Challenge: mcpreceiver.WrongEcho()})
		resp, err := r.c.Subscribe(r.ctx, r.params(u, mcpclient.NewSecret(), r.o.Arguments))
		switch {
		case err != nil:
			r.fail("subscribe.challenge-failed", "request: %v", err)
		case resp.Error == nil:
			r.fail("subscribe.challenge-failed", "subscribe succeeded although the callback echoed a wrong challenge")
		case resp.Error.Code != mcpclient.CodeCallbackEndpointError || resp.Error.DataMap()["reason"] != "challenge_failed":
			r.fail("subscribe.challenge-failed", "want CallbackEndpointError (-32027) with reason challenge_failed, got %s", snippet(resp.Raw))
		default:
			r.pass("subscribe.challenge-failed", "")
		}
	}
}

func (r *runner) httpsProbe() {
	id := "subscribe.https-required"
	hc := &http.Client{Timeout: probeTimeout}
	c := mcpclient.New(r.o.ServerURL, r.o.Principal, mcpclient.Stateless(), mcpclient.WithClientInfoName(clientName), mcpclient.WithHTTPClient(hc))
	resp, err := c.Subscribe(r.ctx, r.params(httpProbeURL, mcpclient.NewSecret(), r.o.Arguments))
	switch {
	case err != nil:
		r.warn(id, "no answer within %s: %v", probeTimeout, err)
	case resp.Error != nil && resp.Error.Code == mcpclient.CodeInvalidParams:
		r.pass(id, "")
	case resp.Error != nil && resp.Error.Code == mcpclient.CodeCallbackEndpointError:
		r.warn(id, "%s", httpAccepted)
	case resp.Error == nil && resp.Status == http.StatusOK:
		r.warn(id, "%s", httpAccepted)
		_, _ = r.c.Unsubscribe(r.ctx, mcpclient.UnsubscribeParams{Name: r.event.Name, Arguments: r.o.Arguments, URL: httpProbeURL})
	default:
		r.fail(id, "want InvalidParams (-32602), got http %d %s", resp.Status, snippet(resp.Raw))
	}
}

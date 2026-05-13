package destwebhook

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// WrapTransport is the destregistry.HTTPClientConfig.WrapTransport hook for
// webhook destinations. It installs OnProxyConnectResponse on the underlying
// transport and wraps it in proxyTransport so request- and response-time
// proxy failures get translated into ErrProxyInfra / ErrProxyDestination.
func WrapTransport(transport *http.Transport, proxyURL *url.URL) http.RoundTripper {
	transport.OnProxyConnectResponse = onProxyConnectResponse
	return newProxyTransport(transport, proxyURL)
}

// ErrProxyInfra signals that a webhook delivery failed at the proxy layer
// (proxy auth misconfiguration, proxy unreachable, etc.). The delivery result
// is nacked so the underlying message queue redelivers without recording a
// customer-visible failed attempt.
type ErrProxyInfra struct {
	Underlying error
	DestHost   string
}

func (e *ErrProxyInfra) Error() string {
	if e.DestHost == "" {
		return "proxy infrastructure error"
	}
	return fmt.Sprintf("proxy infrastructure error reaching %s", e.DestHost)
}

func (e *ErrProxyInfra) Unwrap() error {
	return e.Underlying
}

// ErrProxyDestination signals that the proxy reported a failure originating at
// the destination (e.g. upstream DNS lookup failed, upstream connection
// refused, upstream timeout). The delivery result is recorded as a normal
// failed attempt using Code as the classification, with response data
// rewritten so the customer sees a destination-attributed failure rather than
// proxy-attributed details.
//
// Flag and Details capture the raw Envoy diagnostic strings when present.
// They are operator-side metadata: callers attach them to error logs / the
// publish-attempt error payload so operators can grep the precise Envoy
// reason ("upstream_reset_before_response_started{connection_timeout}", etc.)
// without ever exposing them to the customer-visible attempt record.
type ErrProxyDestination struct {
	Underlying error
	Code       string
	DestHost   string
	Flag       string
	Details    string
}

func (e *ErrProxyDestination) Error() string {
	if e.DestHost == "" {
		return e.Code
	}
	return fmt.Sprintf("%s connecting to %s", e.Code, e.DestHost)
}

func (e *ErrProxyDestination) Unwrap() error {
	return e.Underlying
}

// IsProxyInfraError reports whether err is or wraps an ErrProxyInfra.
func IsProxyInfraError(err error) bool {
	var pe *ErrProxyInfra
	return errors.As(err, &pe)
}

// MapEnvoyResponseFlag returns the destination error code corresponding to an
// Envoy response flag. The output vocabulary matches ClassifyNetworkError
// (httphelper.go) so customers see the same codes whether or not a proxy is
// in path. Unhandled flags fall through to "network_error" — operators should
// watch for that code paired with a non-empty flag in the attempt error
// payload as a signal that the mapping needs expansion.
//
// Envoy response flag reference:
// https://www.envoyproxy.io/docs/envoy/latest/configuration/observability/access_log/usage#config-access-log-format-response-flags
func MapEnvoyResponseFlag(flag string) string {
	switch flag {
	case "UF", "UH", "LH":
		// UF: upstream connection failure (TCP dial failed)
		// UH: no healthy upstream
		// LH: failed local health check
		return "connection_refused"
	case "UC", "UR", "LR":
		// UC: upstream connection termination (established then dropped)
		// UR: upstream remote reset
		// LR: local reset
		return "connection_reset"
	case "UT", "SI", "DT", "UMSDR":
		// UT: upstream request timeout
		// SI: stream idle timeout
		// DT: downstream global duration timeout
		// UMSDR: upstream max stream duration reached
		return "timeout"
	case "DF":
		// DF: DNS resolution failure (emitted by dynamic_forward_proxy when
		// the upstream host cannot be resolved). Verified empirically against
		// the reference Envoy config in build/dev/envoy/envoy.yaml.
		return "dns_error"
	case "NR", "NC":
		// NR: no route configured
		// NC: upstream cluster not found
		return "network_unreachable"
	case "UPE", "DPE":
		// UPE: upstream protocol error
		// DPE: downstream protocol error
		return "protocol_error"
	default:
		return "network_error"
	}
}

// proxyTransport wraps an http.RoundTripper to translate proxy-originated
// failures into ErrProxyInfra / ErrProxyDestination so the delivery pipeline
// can attribute them correctly.
//
// There are two distinct surfaces where the forward proxy can affect the
// transaction, and the wrapper handles them in two different places:
//
//  1. CONNECT-time (HTTPS path) — Go's transport sends a CONNECT request to
//     the proxy. Whatever the proxy responds with is visible to Outpost via
//     onProxyConnectResponse, installed on the underlying http.Transport.
//     This is where proxy auth (407/401/403), proxy-can't-reach-upstream
//     (5xx with Envoy response flags), and similar are classified.
//
//     Once CONNECT succeeds, a raw TCP tunnel is open. TLS runs end-to-end
//     between Outpost and the destination; the forward proxy is byte-blind
//     to everything that follows. The response we read from base.RoundTrip
//     therefore came entirely from the destination side and is not inspected
//     or sanitized by this wrapper — see the scheme check in RoundTrip.
//
//  2. Plain-HTTP forwarding path — the proxy reads the request, makes the
//     upstream call, and writes the response back to Outpost. The proxy is
//     in the byte path on the return, so we can detect Envoy-synthesized
//     responses (via x-envoy-response-flags) and strip x-envoy-* / server
//     fingerprints.
//
// Dial-to-proxy failures (TCP to the proxy itself fails) are detected here
// from the wrapped *net.OpError whose Op == "proxyconnect" (still emitted by
// Go's stdlib; see proxytransport_pin_test.go for the snapshot guarding the
// wording).
type proxyTransport struct {
	base     http.RoundTripper
	proxyURL *url.URL
}

func newProxyTransport(base http.RoundTripper, proxyURL *url.URL) *proxyTransport {
	return &proxyTransport{base: base, proxyURL: proxyURL}
}

func (t *proxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		if mapped := t.classifyTransportError(err, req); mapped != nil {
			return nil, mapped
		}
		return nil, err
	}

	// HTTPS response: bytes came through an established CONNECT tunnel, end-
	// to-end TLS between Outpost and the destination. The forward proxy
	// physically cannot have read, modified, or synthesized any of these
	// bytes. Any x-envoy-* / server: envoy headers we see belong to the
	// destination (often itself behind Envoy at its edge), not to us.
	// Treating them as ours would destroy destination observability and
	// misattribute genuine destination failures as proxy-reported. Proxy-
	// originated HTTPS failures all happen at CONNECT time and are handled
	// in onProxyConnectResponse before we ever reach this branch.
	if req.URL.Scheme == "https" {
		return resp, nil
	}

	// Plain-HTTP forwarding path: the forward proxy was in the byte path on
	// the return and may have synthesized this response itself.

	// Envoy-synthesized response: x-envoy-response-flags carries a non-empty,
	// non-placeholder value (the placeholder "-" means "no flag fired",
	// i.e. a real upstream response was passed through). Convert to a
	// destination-attributed error so the synthesized body never reaches the
	// delivery record.
	if flag := envoyResponseFlag(resp.Header); flag != "" {
		details := envoyResponseDetails(resp.Header)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return nil, &ErrProxyDestination{
			Underlying: synthesizedErr(flag, details, resp.StatusCode),
			Code:       MapEnvoyResponseFlag(flag),
			DestHost:   destHostFromRequest(req),
			Flag:       flag,
			Details:    details,
		}
	}

	// Real upstream response forwarded by the proxy: strip proxy fingerprints
	// so they aren't persisted in the delivery record. Note: if the
	// destination itself sits behind its own Envoy, this can over-strip the
	// destination's x-envoy-* observability headers on the plain-HTTP path.
	// HTTPS destinations are unaffected because of the byte-transparency
	// branch above.
	sanitizeEnvoyHeaders(resp.Header)
	return resp, nil
}

func destHostFromRequest(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	host := req.URL.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host
}

// sanitizeEnvoyHeaders removes Envoy-fingerprinting headers from a response so
// the destination's identity in the delivery record is not contaminated with
// proxy details. Safe no-op when no envoy headers are present.
func sanitizeEnvoyHeaders(h http.Header) {
	for k := range h {
		// http.Header keys are canonicalized, so the prefix is fixed-case.
		if strings.HasPrefix(k, "X-Envoy-") {
			h.Del(k)
		}
	}
	if strings.EqualFold(h.Get("Server"), "envoy") {
		h.Del("Server")
	}
}

func (t *proxyTransport) classifyTransportError(err error, req *http.Request) error {
	// CONNECT-time errors arrive pre-typed via onProxyConnectResponse — pass
	// through unchanged.
	var infraErr *ErrProxyInfra
	if errors.As(err, &infraErr) {
		return nil
	}
	var destErr *ErrProxyDestination
	if errors.As(err, &destErr) {
		return nil
	}

	destHost := ""
	if req != nil && req.URL != nil {
		destHost = req.URL.Host
	}

	// Dial-to-proxy failure: Go wraps these in &net.OpError{Op: "proxyconnect"}
	// even in current versions. The wrap is the most reliable signal that the
	// proxy itself is unreachable (vs. an arbitrary network error en route to
	// the destination, which would be a destination problem). The wording is
	// pinned by TestProxyTransport_PinProxyconnectWording in
	// proxytransport_pin_test.go — update both together if it ever changes.
	if strings.Contains(err.Error(), "proxyconnect") {
		return &ErrProxyInfra{Underlying: err, DestHost: destHost}
	}

	return nil
}

// onProxyConnectResponse is installed on the underlying http.Transport and
// fires for every CONNECT response from the proxy. Non-200 responses are
// translated into the appropriate sentinel here, where the full response
// (status code + headers, including x-envoy-response-flags) is still
// available.
func onProxyConnectResponse(ctx context.Context, proxyURL *url.URL, connectReq *http.Request, resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// CONNECT requests set the target as Request.Host (and URL.Opaque), not
	// URL.Host. See net/http transport.go: connectReq is built with
	// URL: &url.URL{Opaque: targetAddr}, Host: targetAddr.
	destHost := ""
	if connectReq != nil {
		switch {
		case connectReq.Host != "":
			destHost = connectReq.Host
		case connectReq.URL != nil:
			destHost = connectReq.URL.Host
		}
	}
	if h, _, err := net.SplitHostPort(destHost); err == nil {
		destHost = h
	}

	switch resp.StatusCode {
	case http.StatusProxyAuthRequired,
		http.StatusUnauthorized,
		http.StatusForbidden:
		// Auth-related failures are operator misconfiguration of proxy
		// credentials — proxy infrastructure problem, not destination.
		return &ErrProxyInfra{
			Underlying: fmt.Errorf("proxy returned %s", resp.Status),
			DestHost:   destHost,
		}
	}

	// Other non-200 statuses indicate the proxy could not establish the tunnel
	// to the destination. Attribute to destination; refine the code from the
	// Envoy response flag when present.
	code := "connection_refused"
	flag := envoyResponseFlag(resp.Header)
	details := envoyResponseDetails(resp.Header)
	if flag != "" {
		code = MapEnvoyResponseFlag(flag)
	}

	return &ErrProxyDestination{
		Underlying: fmt.Errorf("proxy returned %s", resp.Status),
		Code:       code,
		DestHost:   destHost,
		Flag:       flag,
		Details:    details,
	}
}

// envoyResponseFlag returns the meaningful value of the x-envoy-response-flags
// header, or "" if the header is absent / placeholder "-" / empty.
func envoyResponseFlag(h http.Header) string {
	v := strings.TrimSpace(h.Get("x-envoy-response-flags"))
	if v == "" || v == "-" {
		return ""
	}
	return v
}

// envoyResponseDetails returns the meaningful value of the
// x-envoy-response-code-details header (stage{reason} form when both are
// present, stage-only otherwise), or "" if the header is absent / empty /
// placeholder "-". Captured for operator-side diagnostics; never inspected
// for classification.
func envoyResponseDetails(h http.Header) string {
	v := strings.TrimSpace(h.Get("x-envoy-response-code-details"))
	if v == "" || v == "-" {
		return ""
	}
	return v
}

// synthesizedErr builds the underlying error wrapped inside ErrProxyDestination
// for an Envoy-synthesized plain-HTTP response. Details are included when
// present so the operator-side log line carries the Envoy reason string.
func synthesizedErr(flag, details string, status int) error {
	if details != "" {
		return fmt.Errorf("proxy synthesized response (flag %s, details %s, status %d)", flag, details, status)
	}
	return fmt.Errorf("proxy synthesized response (flag %s, status %d)", flag, status)
}

package destregistry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

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
type ErrProxyDestination struct {
	Underlying error
	Code       string
	DestHost   string
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
// Envoy response flag. Unknown or empty flags map to "network_error".
//
// Envoy response flag reference:
// https://www.envoyproxy.io/docs/envoy/latest/configuration/observability/access_log/usage#config-access-log-format-response-flags
func MapEnvoyResponseFlag(flag string) string {
	switch flag {
	case "UF", "UH", "UC", "LH", "LR":
		// UF: upstream connection failure
		// UH: no healthy upstream
		// UC: upstream connection termination
		// LH/LR: local health-check failures
		return "connection_refused"
	case "UT", "SI", "DT":
		// UT: upstream request timeout (where supported)
		// SI: stream idle timeout
		// DT: downstream global timeout
		return "timeout"
	case "DC":
		// DC: downstream connection termination — surface as DNS-style failure
		// where the dynamic_forward_proxy filter cannot resolve upstream.
		return "dns_error"
	case "NR", "NC":
		// NR: no route configured
		// NC: upstream cluster not found
		return "network_unreachable"
	default:
		return "network_error"
	}
}

// proxyTransport wraps an http.RoundTripper to translate proxy-originated
// failures into ErrProxyInfra / ErrProxyDestination so the delivery pipeline
// can attribute them correctly.
//
// CONNECT-time errors are handled by installing onProxyConnectResponse on the
// underlying http.Transport: that callback sees the proxy's actual response
// (status + headers) before Go discards it, so we can build the right
// sentinel directly. Dial-to-proxy failures are detected here from the
// wrapped *net.OpError whose Op == "proxyconnect" (still set by Go stdlib).
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
	return resp, nil
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
	// the destination, which would be a destination problem).
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
	if flag := envoyResponseFlag(resp.Header); flag != "" {
		code = MapEnvoyResponseFlag(flag)
	}

	return &ErrProxyDestination{
		Underlying: fmt.Errorf("proxy returned %s", resp.Status),
		Code:       code,
		DestHost:   destHost,
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

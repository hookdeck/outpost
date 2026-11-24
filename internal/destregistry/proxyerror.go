package destregistry

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
)

// ErrProxyInfra signals that a delivery failed at the proxy layer
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
// Diagnostics is a free-form key/value map of proxy-specific signals the
// classification path picked up (e.g. for Envoy, "envoy_flag" and
// "envoy_details"). It is operator-side metadata only: surfaced in error
// logs and on the publish-attempt error payload, never written to the
// customer-visible attempt record. Whichever proxy emitted the data owns
// the key naming so heterogeneous proxies can coexist without colliding.
type ErrProxyDestination struct {
	Underlying  error
	Code        string
	DestHost    string
	Diagnostics map[string]string
}

func (e *ErrProxyDestination) Error() string {
	msg := e.Code
	if e.DestHost != "" {
		msg = fmt.Sprintf("%s connecting to %s", e.Code, e.DestHost)
	}
	// Append proxy diagnostics so they surface in zap.Error(err) logs at the
	// consumer boundary. Customer-visible attempt code is still just e.Code.
	if len(e.Diagnostics) == 0 {
		return msg
	}
	keys := make([]string, 0, len(e.Diagnostics))
	for k := range e.Diagnostics {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, e.Diagnostics[k]))
	}
	return fmt.Sprintf("%s (%s)", msg, strings.Join(parts, ", "))
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
// Envoy response flag. The output vocabulary matches the webhook provider's
// ClassifyNetworkError so customers see the same codes whether or not a proxy is
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

// ClassifyProxyConnectResponse maps a non-200 CONNECT response from a proxy
// hop into ErrProxyInfra or ErrProxyDestination. target is the host:port the
// CONNECT was for.
func ClassifyProxyConnectResponse(status int, header http.Header, underlying error, target string) error {
	destHost := hostOnly(target)
	flag := EnvoyResponseFlag(header)
	details := EnvoyResponseDetails(header)

	switch status {
	case http.StatusProxyAuthRequired,
		http.StatusUnauthorized,
		http.StatusForbidden:
		// Envoy RBAC denies (the egress SSRF gate) are a property of the
		// target, not of our credentials: a failed attempt, not a nack.
		if status == http.StatusForbidden && strings.Contains(details, "rbac_access_denied") {
			return &ErrProxyDestination{
				Underlying:  underlying,
				Code:        "network_unreachable",
				DestHost:    destHost,
				Diagnostics: EnvoyDiagnostics(flag, details),
			}
		}
		// Auth-related failures are operator misconfiguration of proxy
		// credentials — proxy infrastructure problem, not destination.
		return &ErrProxyInfra{
			Underlying: underlying,
			DestHost:   destHost,
		}
	}

	// Other non-200 statuses indicate the proxy could not establish the tunnel
	// to the destination. Attribute to destination; refine the code from the
	// Envoy response flag when present.
	code := "connection_refused"
	if flag != "" {
		code = MapEnvoyResponseFlag(flag)
	}
	return &ErrProxyDestination{
		Underlying:  underlying,
		Code:        code,
		DestHost:    destHost,
		Diagnostics: EnvoyDiagnostics(flag, details),
	}
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

// EnvoyResponseFlag returns the meaningful value of the x-envoy-response-flags
// header, or "" if the header is absent / placeholder "-" / empty.
func EnvoyResponseFlag(h http.Header) string {
	v := strings.TrimSpace(h.Get("x-envoy-response-flags"))
	if v == "" || v == "-" {
		return ""
	}
	return v
}

// EnvoyResponseDetails returns the meaningful value of the
// x-envoy-response-code-details header (stage{reason} form when both are
// present, stage-only otherwise), or "" if the header is absent / empty /
// placeholder "-". Captured for operator-side diagnostics; never inspected
// for classification.
func EnvoyResponseDetails(h http.Header) string {
	v := strings.TrimSpace(h.Get("x-envoy-response-code-details"))
	if v == "" || v == "-" {
		return ""
	}
	return v
}

// EnvoyDiagnostics returns the diagnostics map for an Envoy-attributed
// destination error. Returns nil when both inputs are empty so the caller
// gets a properly-zero Diagnostics field (len-0 nil map).
func EnvoyDiagnostics(flag, details string) map[string]string {
	if flag == "" && details == "" {
		return nil
	}
	d := make(map[string]string, 2)
	if flag != "" {
		d["envoy_flag"] = flag
	}
	if details != "" {
		d["envoy_details"] = details
	}
	return d
}

// IsProxyError reports whether err carries a proxy failure from a
// ProxyDialFunc connection.
func IsProxyError(err error) bool {
	var infraErr *ErrProxyInfra
	var destErr *ErrProxyDestination
	return errors.As(err, &infraErr) || errors.As(err, &destErr)
}

// ProxyPublishResult is a publisher's result for a proxy failure (see
// IsProxyError): ErrProxyInfra returns a nil Delivery so the message is
// nacked, ErrProxyDestination a failed attempt with the proxy's code.
func ProxyPublishResult(err error, provider string) (*Delivery, error) {
	var infraErr *ErrProxyInfra
	if errors.As(err, &infraErr) {
		return nil, NewErrDestinationPublishAttempt(err, provider, map[string]interface{}{
			"error":   "proxy_infrastructure",
			"message": infraErr.Error(),
		})
	}
	var destErr *ErrProxyDestination
	if !errors.As(err, &destErr) {
		return nil, err
	}
	data := map[string]interface{}{
		"error":   "connection_failed",
		"message": err.Error(),
	}
	for k, v := range destErr.Diagnostics {
		data[k] = v
	}
	message := destErr.Code
	if destErr.DestHost != "" {
		message = fmt.Sprintf("%s connecting to %s", destErr.Code, destErr.DestHost)
	}
	return &Delivery{
		Status:   "failed",
		Code:     destErr.Code,
		Response: map[string]interface{}{"error": message},
	}, NewErrDestinationPublishAttempt(err, provider, data)
}

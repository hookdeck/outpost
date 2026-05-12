package destregistry

import (
	"errors"
	"fmt"
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

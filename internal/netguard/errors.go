package netguard

import (
	"errors"
	"net/netip"
	"strconv"
)

// Reasons an *AddressNotAllowedError carries.
const (
	// ReasonNotGlobal: an address is neither globally reachable nor
	// allowlisted.
	ReasonNotGlobal = "not_global"
	// ReasonHTTPSRequired: the URL is not https and its addresses don't
	// qualify for plain http (allowlisted loopback, or allowlisted with
	// AllowInsecure).
	ReasonHTTPSRequired = "https_required"
	// ReasonNoAddresses: the host resolved to no address at all.
	ReasonNoAddresses = "no_addresses"
)

// AddressNotAllowedError is returned when the guard refuses a URL or a
// connection. It is for server-side handling and logs only: callers map it
// to a fixed client-visible result and never echo Addr, which can reveal
// internal DNS. Error() leaves Addr out for the same reason.
type AddressNotAllowedError struct {
	// Host is the host as given (URL host or dial host), without port.
	Host string
	// Addr is the refused address; zero when no address is involved
	// (ReasonNoAddresses, or http refused before resolving).
	Addr   netip.Addr
	Reason string
}

func (e *AddressNotAllowedError) Error() string {
	return "netguard: address not allowed for host " + strconv.Quote(e.Host) + ": " + e.Reason
}

// AsAddressNotAllowed returns the *AddressNotAllowedError in err's chain.
func AsAddressNotAllowed(err error) (*AddressNotAllowedError, bool) {
	var e *AddressNotAllowedError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// IsAddressNotAllowed reports whether err's chain holds an
// *AddressNotAllowedError (through *url.Error and *net.OpError too).
func IsAddressNotAllowed(err error) bool {
	_, ok := AsAddressNotAllowed(err)
	return ok
}

// IsHTTPSRequired reports whether err refuses a plain-http URL or
// connection (ReasonHTTPSRequired).
func IsHTTPSRequired(err error) bool {
	e, ok := AsAddressNotAllowed(err)
	return ok && e.Reason == ReasonHTTPSRequired
}

package destregistry

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
)

// CloudEndpointPolicy limits connections made by cloud destination SDKs. Its
// zero value permits public destinations only. Exceptions are deployment
// configuration, never destination configuration supplied by a tenant.
type CloudEndpointPolicy struct {
	allowed []netip.Prefix
}

// NewCloudEndpointPolicy accepts comma-separated CIDRs for operator-approved
// private endpoints, such as a VPC endpoint or a local development emulator.
func NewCloudEndpointPolicy(allowedCIDRs string) (*CloudEndpointPolicy, error) {
	p := &CloudEndpointPolicy{}
	for _, value := range strings.Split(allowedCIDRs, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("invalid cloud destination CIDR %q", value)
		}
		p.allowed = append(p.allowed, prefix.Masked())
	}
	return p, nil
}

// These special-use ranges are not covered by IsPrivate/IsGlobalUnicast.
// IPv6 transition ranges are excluded because they can embed a private IPv4
// destination. IPv4-mapped IPv6 is checked as IPv4 instead.
var cloudSpecialUseRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("168.63.129.16/32"), // Azure platform virtual IP.
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

var cloudGlobalIPv6 = netip.MustParsePrefix("2000::/3")

func (p *CloudEndpointPolicy) allows(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	if p != nil {
		for _, prefix := range p.allowed {
			if prefix.Contains(addr) {
				return true
			}
		}
	}
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	if addr.Is6() && !cloudGlobalIPv6.Contains(addr) {
		return false
	}
	for _, prefix := range cloudSpecialUseRanges {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func cloudEndpointError(field, kind string) error {
	return NewErrDestinationValidation([]ValidationErrorDetail{{Field: field, Type: kind}})
}

func (p *CloudEndpointPolicy) validateHost(host, field string) error {
	if host == "" || strings.ContainsAny(host, "%/@?#\\") {
		return cloudEndpointError(field, "pattern")
	}
	if addr, err := netip.ParseAddr(host); err == nil && !p.allows(addr) {
		return cloudEndpointError(field, "forbidden")
	}
	// Hostnames are deliberately not resolved here. The connection checks the
	// resolved address immediately before connect, including after redirects.
	return nil
}

// ValidateHTTPURL rejects malformed URLs and forbidden literal addresses.
// Dial-time checks are still required for hostnames and existing records.
func (p *CloudEndpointPolicy) ValidateHTTPURL(raw, field string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Opaque != "" {
		return cloudEndpointError(field, "pattern")
	}
	return p.validateHost(u.Hostname(), field)
}

// ValidateGRPCTarget accepts emulator host:port targets only. Arbitrary gRPC
// resolver schemes (for example unix sockets) are not destination endpoints.
func (p *CloudEndpointPolicy) ValidateGRPCTarget(target, field string) error {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return cloudEndpointError(field, "pattern")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return cloudEndpointError(field, "pattern")
	}
	return p.validateHost(host, field)
}

// control runs after DNS resolution for every connection attempt. Inspecting
// the address supplied to connect avoids a DNS validation/use race.
func (p *CloudEndpointPolicy) control(_ context.Context, network, address string, _ syscall.RawConn) error {
	if network != "tcp4" && network != "tcp6" {
		return fmt.Errorf("cloud destination requires TCP")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid cloud destination address: %w", err)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !p.allows(addr) {
		return fmt.Errorf("cloud destination address %q is not permitted", host)
	}
	return nil
}

// DialContext is also used as the Pub/Sub gRPC context dialer. A custom gRPC
// dialer disables environment-proxy resolution and always uses TCP here.
func (p *CloudEndpointPolicy) DialContext(ctx context.Context, address string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, ControlContext: p.control}
	return d.DialContext(ctx, "tcp", address)
}

// AWSHTTPClient retains SDK TLS, pooling and redirect behavior, with the same
// destination policy applied to each connection. Environment proxies are
// disabled: a proxy resolving the destination would bypass the local IP check.
func (p *CloudEndpointPolicy) AWSHTTPClient() *awshttp.BuildableClient {
	return awshttp.NewBuildableClient().WithDialerOptions(func(d *net.Dialer) {
		d.ControlContext = p.control
	}).WithTransportOptions(func(tr *http.Transport) {
		tr.Proxy = nil
	})
}

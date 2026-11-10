package destregistry

import (
	"context"
	"errors"
	"net"
	"net/url"
	"time"

	"github.com/hookdeck/outpost/internal/proxychain"
)

// ProxyDialFunc returns a dial func that tunnels through hops, nearest first,
// or nil when hops is empty. A hop refusing the CONNECT is classified by
// ProxyHopPolicy.ClassifyConnect. A CONNECT timing out passes through as a
// timeout, and cancellation passes through unchanged. Any other failure (a
// hop unreachable, TLS to a hop, a hop hanging up) is ErrProxyInfra, except
// at the last externalHops hops, where it is a failed attempt (see
// ProxyHopPolicy).
func ProxyDialFunc(hops []*url.URL, externalHops int) proxychain.DialFunc {
	if len(hops) == 0 {
		return nil
	}
	policy := NewProxyHopPolicy(hops, externalHops)
	firstHop := proxychain.Redact(hops[0])
	base := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	dialer := proxychain.NewDialer(hops, func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := base.DialContext(ctx, network, addr)
		if err != nil {
			return nil, &ErrProxyInfra{Underlying: err, Hop: firstHop}
		}
		return conn, nil
	}, nil)
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, addr)
		if err == nil {
			return conn, nil
		}
		var infraErr *ErrProxyInfra
		if errors.As(err, &infraErr) {
			infraErr.DestHost = hostOnly(addr)
			if errors.Is(err, context.Canceled) {
				return nil, infraErr
			}
			return nil, policy.Apply(infraErr)
		}
		var connectErr *proxychain.ConnectError
		if errors.As(err, &connectErr) {
			return nil, policy.ClassifyConnect(connectErr.Status, connectErr.Header, err, addr, connectErr.Index, connectErr.Hop)
		}
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		// A CONNECT that goes unanswered looks the same whether the hop or
		// the target behind it is silent, so it stays a timeout attempt. A
		// TLS handshake with a hop is the hop alone.
		var hopErr *proxychain.HopError
		var netErr net.Error
		if !(errors.As(err, &hopErr) && hopErr.Handshake) && errors.As(err, &netErr) && netErr.Timeout() {
			return nil, err
		}
		infra := &ErrProxyInfra{Underlying: err, DestHost: hostOnly(addr)}
		if hopErr != nil {
			infra.HopIndex, infra.Hop = hopErr.Index, hopErr.Hop
		}
		return nil, policy.Apply(infra)
	}
}

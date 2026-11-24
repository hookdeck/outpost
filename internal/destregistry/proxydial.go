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
// ClassifyProxyConnectResponse. A CONNECT timing out passes through as a
// timeout, and cancellation passes through unchanged. Any other failure (a
// hop unreachable, TLS to a hop, a hop hanging up) is ErrProxyInfra.
func ProxyDialFunc(hops []*url.URL) proxychain.DialFunc {
	if len(hops) == 0 {
		return nil
	}
	base := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	dialer := proxychain.NewDialer(hops, func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := base.DialContext(ctx, network, addr)
		if err != nil {
			return nil, &ErrProxyInfra{Underlying: err}
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
			return nil, infraErr
		}
		var connectErr *proxychain.ConnectError
		if errors.As(err, &connectErr) {
			return nil, ClassifyProxyConnectResponse(connectErr.Status, connectErr.Header, err, connectErr.Next)
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
		return nil, &ErrProxyInfra{Underlying: err, DestHost: hostOnly(addr)}
	}
}

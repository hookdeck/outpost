package destregistry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloudEndpointPolicyAddresses(t *testing.T) {
	p, err := NewCloudEndpointPolicy("")
	require.NoError(t, err)
	for _, ip := range []string{"0.0.0.0", "0.1.2.3", "10.0.0.1", "127.0.0.1", "169.254.169.254", "172.16.0.1", "192.168.1.1", "100.100.100.200", "198.18.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "168.63.129.16", "::", "::1", "::ffff:127.0.0.1", "fd00:ec2::254", "fe80::1", "ff02::1", "64:ff9b::7f00:1", "2001:db8::1", "2002:7f00:1::1"} {
		t.Run(ip, func(t *testing.T) { require.False(t, p.allows(netip.MustParseAddr(ip))) })
	}
	for _, ip := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "::ffff:8.8.8.8"} {
		require.True(t, p.allows(netip.MustParseAddr(ip)), ip)
	}
	p, err = NewCloudEndpointPolicy(" 10.1.2.0/24, 127.0.0.1/32, fd12::/64 ")
	require.NoError(t, err)
	require.True(t, p.allows(netip.MustParseAddr("10.1.2.3")))
	require.True(t, p.allows(netip.MustParseAddr("::ffff:127.0.0.1")))
	require.True(t, p.allows(netip.MustParseAddr("fd12::1")))
	require.False(t, p.allows(netip.MustParseAddr("10.1.3.1")))
	require.False(t, p.allows(netip.MustParseAddr("127.0.0.2")))
	for _, cidr := range []string{"localhost", "10.0.0.1", "10.0.0.0/33", "::ffff:127.0.0.0/104"} {
		_, err := NewCloudEndpointPolicy(cidr)
		require.Error(t, err, cidr)
	}
}

func TestCloudEndpointPolicyTargets(t *testing.T) {
	var p *CloudEndpointPolicy // nil is the secure default used by providers.
	for _, raw := range []string{"http://127.0.0.1", "http://[::ffff:127.0.0.1]", "http://[fe80::1%25eth0]", "http://", "http:opaque", "file:///etc/passwd", "https://user:password@example.com"} {
		require.Error(t, p.ValidateHTTPURL(raw, "config.endpoint"), raw)
	}
	for _, raw := range []string{"https://events.us-east-1.amazonaws.com", "https://custom.example.com:8443/api", "https://8.8.8.8", "https://[2606:4700:4700::1111]"} {
		require.NoError(t, p.ValidateHTTPURL(raw, "config.endpoint"), raw)
	}
	for _, target := range []string{"127.0.0.1:8085", "[::1]:8085", "unix:///tmp/service.sock", "dns:///localhost:8085", "passthrough:///localhost:8085", "localhost", ":8085", "localhost:0", "localhost:65536"} {
		require.Error(t, p.ValidateGRPCTarget(target, "config.endpoint"), target)
	}
	require.NoError(t, p.ValidateGRPCTarget("pubsub.example.com:443", "config.endpoint"))
}

func TestCloudEndpointPolicyResolvedConnection(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer s.Close()
	var p *CloudEndpointPolicy
	// A hostname passes config validation, but its resolved loopback address
	// must be denied at connect time, before any bytes reach the receiver.
	target := strings.Replace(s.URL, "127.0.0.1", "localhost", 1)
	require.NoError(t, p.ValidateHTTPURL(target, "config.endpoint"))
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader("test"))
	require.NoError(t, err)
	_, err = p.AWSHTTPClient().WithTimeout(time.Second).Do(req)
	require.ErrorContains(t, err, "not permitted")
	require.Zero(t, hits.Load())
	conn, err := p.DialContext(context.Background(), strings.TrimPrefix(target, "http://"))
	require.ErrorContains(t, err, "not permitted")
	require.Nil(t, conn)
	// Control is called again for each resolved connection address; an earlier
	// permitted answer does not authorize a later private DNS answer.
	require.NoError(t, p.control(context.Background(), "tcp4", "8.8.8.8:443", nil))
	require.Error(t, p.control(context.Background(), "tcp4", "127.0.0.1:443", nil))
}

func TestCloudEndpointPolicyRedirectAndProxy(t *testing.T) {
	var forbiddenHits, proxyHits atomic.Int32
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forbiddenHits.Add(1) }))
	defer forbidden.Close()
	// Use a forbidden unspecified address with the test receiver's port. A
	// second loopback IP would require an explicit interface alias on macOS.
	forbiddenURL := strings.Replace(forbidden.URL, "127.0.0.1", "0.0.0.0", 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyHits.Add(1) }))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	p, err := NewCloudEndpointPolicy("127.0.0.1/32")
	require.NoError(t, err)
	require.Nil(t, p.AWSHTTPClient().GetTransport().Proxy)
	for _, status := range []int{307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, forbiddenURL, status)
			}))
			defer first.Close()
			req, err := http.NewRequest(http.MethodPost, first.URL, strings.NewReader("test"))
			require.NoError(t, err)
			_, err = p.AWSHTTPClient().WithTimeout(time.Second).Do(req)
			require.ErrorContains(t, err, "not permitted")
		})
	}
	require.Zero(t, forbiddenHits.Load())
	require.Zero(t, proxyHits.Load())
	allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "allowed") }))
	defer allowed.Close()
	req, err := http.NewRequest(http.MethodGet, allowed.URL, nil)
	require.NoError(t, err)
	resp, err := p.AWSHTTPClient().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "allowed", string(body))
}

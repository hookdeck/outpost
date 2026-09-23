package mqs_test

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/proxychain"
	"github.com/hookdeck/outpost/internal/proxychain/proxychaintest"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var amqpProtocolHeader = []byte("AMQP\x00\x00\x09\x01")

// newFakeBroker accepts one connection, reads the AMQP protocol header the
// client sends first, reports it and hangs up. With tlsConfig set it
// terminates TLS first, so the header only arrives if the client's TLS
// session reached the broker itself.
func newFakeBroker(t *testing.T, tlsConfig *tls.Config) (string, <-chan []byte) {
	t.Helper()
	var ln net.Listener
	var err error
	if tlsConfig != nil {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	got := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, len(amqpProtocolHeader))
		if _, err := io.ReadFull(conn, buf); err == nil {
			got <- buf
		}
	}()
	return ln.Addr().String(), got
}

func proxyDial(t *testing.T, chain string) proxychain.DialFunc {
	t.Helper()
	hops, err := proxychain.Parse(chain)
	require.NoError(t, err)
	return proxychain.NewDialer(hops, nil, nil).DialContext
}

func initRabbitMQ(serverURL string, dial proxychain.DialFunc) error {
	queue := mqs.NewRabbitMQQueue(&mqs.RabbitMQConfig{ServerURL: serverURL, Queue: "test", Dial: dial})
	cleanup, err := queue.Init(context.Background())
	if err == nil {
		cleanup()
	}
	return err
}

func receiveHeader(t *testing.T, got <-chan []byte) []byte {
	t.Helper()
	select {
	case b := <-got:
		return b
	case <-time.After(5 * time.Second):
		t.Fatal("broker never received the AMQP protocol header")
		return nil
	}
}

func TestMQ_RabbitMQDialsThroughProxyChain(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d hops", n), func(t *testing.T) {
			t.Parallel()
			broker, got := newFakeBroker(t, nil)
			hops := make([]*proxychaintest.Proxy, n)
			urls := make([]string, n)
			for i := range hops {
				hops[i] = proxychaintest.New(t, false)
				urls[i] = hops[i].URL
			}

			// The fake broker hangs up after the protocol header, so the dial fails.
			require.Error(t, initRabbitMQ("amqp://guest:guest@"+broker+"/", proxyDial(t, strings.Join(urls, " "))))
			assert.Equal(t, amqpProtocolHeader, receiveHeader(t, got))
			assert.Equal(t, []string{broker}, hops[n-1].Connects(), "last hop tunnels to the broker")
			if n == 2 {
				assert.Equal(t, []string{proxychaintest.HostOf(urls[1])}, hops[0].Connects())
			}
		})
	}
}

func TestMQ_RabbitMQProxyAMQPS(t *testing.T) {
	t.Parallel()
	certSrv := httptest.NewTLSServer(http.NotFoundHandler())
	certSrv.Close()
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certSrv.Certificate().Raw}), 0o600))

	broker, got := newFakeBroker(t, &tls.Config{Certificates: certSrv.TLS.Certificates})
	hop := proxychaintest.New(t, false)

	serverURL := "amqps://guest:guest@" + broker + "/?cacertfile=" + caFile
	require.Error(t, initRabbitMQ(serverURL, proxyDial(t, hop.URL)))
	assert.Equal(t, amqpProtocolHeader, receiveHeader(t, got), "TLS runs end to end from client to broker through the tunnel")
	assert.Equal(t, []string{broker}, hop.Connects())
}

func TestMQ_RabbitMQProxyAuthRejected(t *testing.T) {
	t.Parallel()
	hop := proxychaintest.New(t, false)
	hop.Reject = func(string) (int, http.Header) { return http.StatusProxyAuthRequired, nil }

	chain := proxychaintest.WithCreds(hop.URL, "u", "topsecret")
	err := initRabbitMQ("amqp://guest:guest@127.0.0.1:5672/", proxyDial(t, chain))
	var connectErr *proxychain.ConnectError
	require.True(t, errors.As(err, &connectErr), "got %v", err)
	assert.Equal(t, http.StatusProxyAuthRequired, connectErr.Status)
	assert.Equal(t, "http://"+proxychaintest.HostOf(hop.URL), connectErr.Hop)
	assert.NotContains(t, err.Error(), "topsecret")
}

func TestMQ_RabbitMQProxyHonoursConnectionTimeout(t *testing.T) {
	t.Parallel()
	hop := proxychaintest.New(t, false)
	hop.Hold = make(chan struct{})
	t.Cleanup(func() { close(hop.Hold) })

	start := time.Now()
	err := initRabbitMQ("amqp://guest:guest@127.0.0.1:5672/?connection_timeout=200", proxyDial(t, hop.URL))
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestIntegrationMQ_RabbitMQPublishThroughProxyChain(t *testing.T) {
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	config := testinfra.NewMQRabbitMQConfig(t)
	hop0 := proxychaintest.New(t, false)
	hop1 := proxychaintest.New(t, false)
	config.RabbitMQ.Dial = proxyDial(t, hop0.URL+" "+hop1.URL)

	ctx := context.Background()
	queue := mqs.NewQueue(&config)
	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	subscription, err := queue.Subscribe(ctx)
	require.NoError(t, err)
	defer subscription.Shutdown(ctx)

	require.NoError(t, queue.Publish(ctx, &Msg{ID: "via-proxy"}))
	receiveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	msg, err := subscription.Receive(receiveCtx)
	require.NoError(t, err)
	msg.Ack()
	parsed := &Msg{}
	require.NoError(t, parsed.FromMessage(msg))
	assert.Equal(t, "via-proxy", parsed.ID)

	assert.Len(t, hop0.Connects(), 1, "one AMQP connection serves publish and subscribe")
	assert.Equal(t, []string{proxychaintest.HostOf(hop1.URL)}, hop0.Connects())
}

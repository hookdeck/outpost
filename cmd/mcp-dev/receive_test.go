package main

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func freeAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	return ln.Addr().String()
}

func TestReceiveIgnoresStrayPathsWithoutRacing(t *testing.T) {
	addr := freeAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- newApp().Run(ctx, []string{"mcp-dev", "receive", "--listen", addr, "--public-url", "http://" + addr, "--secret", "whsec_test"})
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			c.Close()
			break
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, p := range []string{"/favicon.ico", "/robots.txt"} {
				if resp, err := http.Post("http://"+addr+p, "application/json", strings.NewReader(`{}`)); err == nil {
					resp.Body.Close()
				}
			}
		}()
	}
	wg.Wait()

	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("receive did not stop")
	}
}

func TestDescribeChallengeAnswer(t *testing.T) {
	body := []byte(`{"type":"verification","challenge":"c"}`)
	signed := func(secret string) mcpreceiver.Request {
		h := http.Header{}
		h.Set("webhook-id", "msg_1")
		h.Set("webhook-timestamp", "1700000000")
		h.Set("webhook-signature", mcpreceiver.Sign(secret, "msg_1", "1700000000", body))
		return mcpreceiver.Request{Header: h, Body: body}
	}
	assert.Contains(t, describe(signed("whsec_a"), []string{"whsec_a"}), "-> echoed challenge")
	assert.Contains(t, describe(signed("whsec_a"), []string{"whsec_b"}), "-> 401")
}

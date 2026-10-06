package testinfra

import (
	"net"
	"testing"
	"time"
)

// An external endpoint that refuses connections while its service boots, as
// a published port does without Docker's userland proxy, is waited for.
func TestServiceWaitsForExternalEndpointThatStartsLate(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	lateCh := make(chan net.Listener, 1)
	t.Cleanup(func() {
		if late := <-lateCh; late != nil {
			late.Close()
		}
	})
	go func() {
		time.Sleep(3 * time.Second)
		late, err := net.Listen("tcp", addr)
		lateCh <- late
		if err != nil {
			return
		}
		for {
			conn, err := late.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	s := &service{name: "late", startHint: hintTest}
	start := time.Now()
	got := s.ensure(t, addr, nil, dialTCP)
	if got != addr {
		t.Fatalf("endpoint = %q, want %q", got, addr)
	}
	if elapsed := time.Since(start); elapsed < 3*time.Second {
		t.Fatalf("returned after %s, before the service listened", elapsed)
	}
}

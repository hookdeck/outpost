package testinfra

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"
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
	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		if late := <-lateCh; late != nil {
			late.Close()
		}
	})
	refusals := make(chan struct{}, 2)
	go func() {
		for range 2 {
			select {
			case <-refusals:
			case <-stop:
				lateCh <- nil
				return
			}
		}
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
	var probes atomic.Int32
	got := s.ensure(t, addr, nil, func(endpoint string) error {
		err := dialRefusingSelfConnect(endpoint)
		probes.Add(1)
		if err != nil {
			select {
			case refusals <- struct{}{}:
			default:
			}
		}
		return err
	})
	if got != addr {
		t.Fatalf("endpoint = %q, want %q", got, addr)
	}
	if n := probes.Load(); n < 3 {
		t.Fatalf("returned after %d probes, before the service listened", n)
	}
}

// dialRefusingSelfConnect is dialTCP, except a dial that connected the client
// to itself counts as a failure. Linux does that when the target port is in
// the ephemeral range and nothing listens there.
func dialRefusingSelfConnect(endpoint string) error {
	conn, err := net.Dial("tcp", endpoint)
	if err != nil {
		return err
	}
	defer conn.Close()
	if conn.LocalAddr().String() == conn.RemoteAddr().String() {
		return errors.New("connected to itself")
	}
	return nil
}

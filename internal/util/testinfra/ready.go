package testinfra

import (
	"fmt"
	"log"
	"net"
	"time"
)

// readyTimeout bounds how long a service gets to start accepting connections.
// Generous enough for a container that is still booting, short enough that a
// service which is never coming back fails the run instead of hanging it.
const readyTimeout = 30 * time.Second

// waitReady blocks until probe succeeds, and panics if it has not succeeded
// within readyTimeout.
//
// Services started by testcontainers are already gated by that library's wait
// strategies. Services supplied through the environment — the compose stack
// behind TESTINFRA=1 — are not: `docker compose up -d` returns once containers
// are created, not once they accept connections, so a suite that connects
// immediately can lose the race. Without this, that race surfaces inside a test
// as a connection reset partway through, which reads as a flaky test rather
// than as infrastructure that was not ready.
func waitReady(name, endpoint string, probe func() error) {
	deadline := time.Now().Add(readyTimeout)
	var lastErr error
	for {
		if lastErr = probe(); lastErr == nil {
			return
		}
		if time.Now().After(deadline) {
			panic(fmt.Errorf("%s at %s not ready after %s: %w", name, endpoint, readyTimeout, lastErr))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// waitReadyLogged is waitReady with a note when the service was not immediately
// available, so a slow start is visible in test output rather than looking like
// an unexplained pause.
func waitReadyLogged(name, endpoint string, probe func() error) {
	if probe() == nil {
		return
	}
	log.Printf("waiting for %s at %s", name, endpoint)
	start := time.Now()
	waitReady(name, endpoint, probe)
	log.Printf("%s ready after %s", name, time.Since(start).Round(time.Millisecond))
}

// dialTCP reports whether endpoint accepts TCP connections. It is the right
// probe for a service whose readiness is "the port is open", and the wrong one
// for anything that needs a protocol handshake to be meaningfully ready.
func dialTCP(endpoint string) error {
	conn, err := net.DialTimeout("tcp", endpoint, 5*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

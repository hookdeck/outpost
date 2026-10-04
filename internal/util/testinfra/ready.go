package testinfra

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// readyTimeout bounds how long a service gets to start accepting connections.
// Generous enough for a container that is still booting, short enough that a
// service which is never coming back fails the run instead of hanging it.
const readyTimeout = 30 * time.Second

// refusedTimeout bounds how long an external (TESTINFRA=1) endpoint may keep
// refusing connections before it counts as not running. With Docker's default
// userland proxy a published port accepts connections as soon as its
// container runs; without it, connections are refused until the service
// listens, so this also has to cover a service that is still booting right
// after `make up/test`.
const refusedTimeout = 15 * time.Second

// service is one piece of shared test infrastructure. Its endpoint and
// readiness are resolved once per test binary; every test that asks afterwards
// gets the same answer, including the same failure.
type service struct {
	name string
	// startHint tells a TESTINFRA=1 user how to start the service when nothing
	// is listening at its endpoint.
	startHint string

	once     sync.Once
	endpoint string
	err      error
}

// ensure returns the service's endpoint: external when set (TESTINFRA=1),
// otherwise the one startContainer returns. It fails t if the service could
// not be started or did not become ready.
func (s *service) ensure(t testing.TB, external string, startContainer func() (string, error), probe func(endpoint string) error) string {
	t.Helper()
	s.once.Do(func() {
		endpoint := external
		if endpoint == "" {
			var err error
			if endpoint, err = startContainer(); err != nil {
				s.err = fmt.Errorf("starting %s container: %w", s.name, err)
				return
			}
		}
		s.endpoint = endpoint
		s.err = s.waitReady(endpoint, external != "", func() error { return probe(endpoint) })
	})
	if s.err != nil {
		t.Fatal(s.err)
	}
	return s.endpoint
}

// waitReady blocks until probe succeeds, and returns an error if it has not
// succeeded within readyTimeout, or if an external endpoint refused every
// connection for refusedTimeout in a row.
//
// Every service runs its probe, however it was provided. Neither way of
// starting one is trustworthy on its own: `docker compose up -d` returns when
// containers are created, not when they accept connections, and a testcontainers
// wait strategy watches a port or a log line, which a service can satisfy while
// still refusing the protocol handshake a test needs. Both surface inside a test
// as a reset or an EOF partway through, which reads as a flaky test rather than
// as infrastructure that was not ready.
//
// This is why the probes speak the protocol rather than dialing the port.
func (s *service) waitReady(endpoint string, external bool, probe func() error) error {
	lastErr := probe()
	if lastErr == nil {
		return nil
	}
	log.Printf("waiting for %s at %s", s.name, endpoint)
	start := time.Now()
	refusedSince := start
	for {
		if external {
			if !refused(endpoint) {
				refusedSince = time.Now()
			} else if time.Since(refusedSince) > refusedTimeout {
				return fmt.Errorf("%s: nothing listening at %s for %s (TESTINFRA=1): not running, or still starting. %s", s.name, endpoint, refusedTimeout, s.startHint)
			}
		}
		if time.Since(start) > readyTimeout {
			return fmt.Errorf("%s at %s not ready after %s: %w", s.name, endpoint, readyTimeout, lastErr)
		}
		time.Sleep(250 * time.Millisecond)
		if lastErr = probe(); lastErr == nil {
			log.Printf("%s ready after %s", s.name, time.Since(start).Round(time.Millisecond))
			return nil
		}
	}
}

// refused reports whether a TCP connection to endpoint is actively refused.
// endpoint is host:port or a URL with one.
func refused(endpoint string) bool {
	addr := endpoint
	if strings.Contains(endpoint, "://") {
		if u, err := url.Parse(endpoint); err == nil {
			addr = u.Host
		}
	}
	return errors.Is(dialTCP(addr), syscall.ECONNREFUSED)
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

// Start hints, shown with TESTINFRA=1 when nothing listens at a service's
// endpoint.
const (
	hintTest = "Start the test stack: `make up/test`."
	hintDest = hintTest
)

package testutil

import (
	"net"
	"strconv"
	"testing"
)

func TestRandomPortNumber_BelowEphemeralRangeAndFree(t *testing.T) {
	for range 50 {
		port := RandomPortNumber()
		if port < minRandomPort || port > maxRandomPort {
			t.Fatalf("port %d outside %d-%d", port, minRandomPort, maxRandomPort)
		}
	}

	// A taken port is skipped.
	port := RandomPortNumber()
	l, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("listen on the returned port: %v", err)
	}
	defer l.Close()
	for range 50 {
		if RandomPortNumber() == port {
			t.Fatalf("returned port %d while it was in use", port)
		}
	}
}

package destkafka_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destkafka"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kmsg"
	"go.uber.org/goleak"
)

type fakeRequest struct {
	key, version int16
	correlation  int32
}

func readFakeRequest(c net.Conn) (fakeRequest, error) {
	var size [4]byte
	if _, err := io.ReadFull(c, size[:]); err != nil {
		return fakeRequest{}, err
	}
	n := binary.BigEndian.Uint32(size[:])
	if n < 8 || n > 1<<20 {
		return fakeRequest{}, io.ErrUnexpectedEOF
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c, body); err != nil {
		return fakeRequest{}, err
	}
	return fakeRequest{
		key:         int16(binary.BigEndian.Uint16(body[0:])),
		version:     int16(binary.BigEndian.Uint16(body[2:])),
		correlation: int32(binary.BigEndian.Uint32(body[4:])),
	}, nil
}

func fakeFrame(correlation int32, body []byte) []byte {
	out := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(out, uint32(4+len(body)))
	binary.BigEndian.PutUint32(out[4:], uint32(correlation))
	return append(out, body...)
}

func fakeAPIVersions(r fakeRequest) []byte {
	resp := kmsg.NewApiVersionsResponse()
	resp.Version = r.version
	for _, k := range []int16{0, 3, 17, 18, 36} {
		ak := kmsg.NewApiVersionsResponseApiKey()
		ak.ApiKey = k
		ak.MaxVersion = 3
		if k == 17 || k == 36 {
			ak.MaxVersion = 1
		}
		resp.ApiKeys = append(resp.ApiKeys, ak)
	}
	return fakeFrame(r.correlation, resp.AppendTo(nil))
}

// serveHostile answers the first request(s) on c with a malformed reply, then
// holds the connection open until the client closes it.
func serveHostile(variant string, c net.Conn) {
	defer c.Close()
	defer func() { _, _ = io.Copy(io.Discard, c) }()

	r, err := readFakeRequest(c)
	if err != nil {
		return
	}
	switch variant {
	case "http":
		_, _ = c.Write([]byte("HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n"))
	case "huge-array":
		// A small, well-formed frame that declares a huge array.
		body := []byte{0, 0}
		if r.version >= 3 {
			body = binary.AppendUvarint(body, 0x7ffffff0)
		} else {
			body = append(body, 0x01, 0x00, 0x00, 0x00)
		}
		_, _ = c.Write(fakeFrame(r.correlation, body))
	case "huge-size":
		_, _ = c.Write([]byte{0x7f, 0xff, 0xff, 0xff})
	case "sasl":
		_, _ = c.Write(fakeAPIVersions(r))
		r2, err := readFakeRequest(c)
		if err != nil {
			return
		}
		// Handshake reply: no error, then a huge mechanisms count.
		_, _ = c.Write(fakeFrame(r2.correlation, []byte{0, 0, 0x7f, 0xff, 0xff, 0xff}))
	}
}

func startHostileBroker(t *testing.T, variant string) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var mu sync.Mutex
	var conns []net.Conn
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				serveHostile(variant, c)
			}()
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	}
}

// A misbehaving or non-Kafka endpoint must not make the client allocate
// unbounded memory, and the delivery must still fail promptly.
func TestKafkaPublisher_HostileBroker(t *testing.T) {
	for _, variant := range []string{"http", "huge-array", "huge-size", "sasl"} {
		t.Run(variant, func(t *testing.T) {
			defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

			addr, stop := startHostileBroker(t, variant)
			defer stop()

			provider, err := destkafka.New(testutil.Registry.MetadataLoader(), nil)
			require.NoError(t, err)
			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("kafka"),
				testutil.DestinationFactory.WithConfig(map[string]string{
					"brokers":        addr,
					"topic":          "test-topic",
					"sasl_mechanism": "plain",
				}),
				testutil.DestinationFactory.WithCredentials(map[string]string{
					"username": "user",
					"password": "pass",
				}),
			)
			publisher, err := provider.CreatePublisher(context.Background(), &destination)
			require.NoError(t, err)
			defer publisher.Close()

			event := testutil.EventFactory.Any(
				testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
			)

			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			start := time.Now()
			delivery, err := publisher.Publish(ctx, &event)
			elapsed := time.Since(start)

			runtime.ReadMemStats(&after)

			require.Error(t, err)
			require.NotNil(t, delivery)
			assert.Equal(t, "failed", delivery.Status)
			assert.Less(t, elapsed, 3*time.Second)
			assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(64<<20), "allocated too much while handling the reply")
			if variant == "http" {
				assert.Equal(t, "kafka_error", delivery.Code)
				assert.Contains(t, delivery.Response["error"], "HTTP")
			}

			require.NoError(t, publisher.Close())
		})
	}
}

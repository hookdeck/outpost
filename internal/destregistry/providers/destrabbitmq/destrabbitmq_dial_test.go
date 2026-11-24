package destrabbitmq_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSilentBroker accepts connections and never answers, so the AMQP
// handshake stalls until the client gives up.
func newSilentBroker(t *testing.T) string {
	addr, _ := newCountingSilentBroker(t)
	return addr
}

// newCountingSilentBroker is newSilentBroker that also signals each accepted
// connection.
func newCountingSilentBroker(t *testing.T) (string, <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	accepted := make(chan struct{}, 16)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			accepted <- struct{}{}
		}
	}()
	return ln.Addr().String(), accepted
}

func newRabbitMQPublisher(t *testing.T, serverURL string) destregistry.Publisher {
	t.Helper()
	provider, err := destrabbitmq.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("rabbitmq"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"server_url": serverURL,
			"exchange":   "test-exchange",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": "guest",
			"password": "guest",
		}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	t.Cleanup(func() { publisher.Close() })
	return publisher
}

func TestRabbitMQPublisher_DialStopsAtDeadline(t *testing.T) {
	t.Parallel()
	publisher := newRabbitMQPublisher(t, newSilentBroker(t))
	event := testutil.EventFactory.Any()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	delivery, err := publisher.Publish(ctx, &event)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second)
	require.NotNil(t, delivery)
	assert.Equal(t, "timeout", delivery.Code)
}

func TestRabbitMQPublisher_LockWaitStopsAtDeadline(t *testing.T) {
	t.Parallel()
	broker, accepted := newCountingSilentBroker(t)
	publisher := newRabbitMQPublisher(t, broker)

	// The first delivery holds the connection lock while its dial stalls.
	holderCtx, cancelHolder := context.WithTimeout(context.Background(), time.Second)
	defer cancelHolder()
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		event := testutil.EventFactory.Any()
		_, _ = publisher.Publish(holderCtx, &event)
	}()
	<-accepted // the holder is dialing, so it holds the lock

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	event := testutil.EventFactory.Any()
	start := time.Now()
	delivery, err := publisher.Publish(ctx, &event)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "waiter gives up at its own deadline")
	require.NotNil(t, delivery)
	assert.Equal(t, "timeout", delivery.Code)

	select {
	case <-accepted:
		t.Fatal("the waiter dialed instead of waiting on the lock")
	default:
	}

	cancelHolder()
	<-holderDone
}

func TestRabbitMQPublisher_CanceledWhileWaitingNacks(t *testing.T) {
	t.Parallel()
	broker, accepted := newCountingSilentBroker(t)
	publisher := newRabbitMQPublisher(t, broker)

	holderCtx, cancelHolder := context.WithTimeout(context.Background(), time.Second)
	defer cancelHolder()
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		event := testutil.EventFactory.Any()
		_, _ = publisher.Publish(holderCtx, &event)
	}()
	<-accepted // the holder is dialing, so it holds the lock

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	event := testutil.EventFactory.Any()
	delivery, err := publisher.Publish(ctx, &event)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, delivery)

	select {
	case <-accepted:
		t.Fatal("the waiter dialed instead of waiting on the lock")
	default:
	}

	cancelHolder()
	<-holderDone
}

// newStallingBroker completes the AMQP handshake, then reads and ignores
// everything, so channel.open (and connection.close) never get a reply.
func newStallingBroker(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if _, err := io.ReadFull(conn, make([]byte, 8)); err != nil { // protocol header
			return
		}
		table := []byte{0, 0, 0, 0}
		start := append([]byte{0, 9}, table...)
		start = append(start, longStr("PLAIN")...)
		start = append(start, longStr("en_US")...)
		writeMethod(conn, 10, 10, start)
		readFrame(conn)                                                  // start-ok
		writeMethod(conn, 10, 30, []byte{0x07, 0xff, 0, 2, 0, 0, 0, 60}) // tune: channel-max 2047, frame-max 131072, heartbeat 60
		readFrame(conn)                                                  // tune-ok
		readFrame(conn)                                                  // open
		writeMethod(conn, 10, 41, []byte{0})                             // open-ok
		_, _ = io.Copy(io.Discard, conn)
	}()
	return ln.Addr().String()
}

func longStr(s string) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(s)))
	return append(b, s...)
}

func writeMethod(w io.Writer, class, method uint16, args []byte) {
	payload := binary.BigEndian.AppendUint16(nil, class)
	payload = binary.BigEndian.AppendUint16(payload, method)
	payload = append(payload, args...)
	frame := []byte{1, 0, 0}
	frame = binary.BigEndian.AppendUint32(frame, uint32(len(payload)))
	frame = append(frame, payload...)
	frame = append(frame, 0xCE)
	_, _ = w.Write(frame)
}

func readFrame(r io.Reader) {
	header := make([]byte, 7)
	if _, err := io.ReadFull(r, header); err != nil {
		return
	}
	size := binary.BigEndian.Uint32(header[3:])
	_, _ = io.ReadFull(r, make([]byte, size+1))
}

func TestRabbitMQPublisher_ChannelOpenStopsAtDeadline(t *testing.T) {
	t.Parallel()
	publisher := newRabbitMQPublisher(t, newStallingBroker(t))
	event := testutil.EventFactory.Any()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	delivery, err := publisher.Publish(ctx, &event)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second)
	require.NotNil(t, delivery)
	assert.Equal(t, "timeout", delivery.Code)
}

func TestRabbitMQPublisher_CanceledMidHandshakeNacks(t *testing.T) {
	t.Parallel()
	broker, accepted := newCountingSilentBroker(t)
	publisher := newRabbitMQPublisher(t, broker)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-accepted
		cancel()
	}()
	event := testutil.EventFactory.Any()
	start := time.Now()
	delivery, err := publisher.Publish(ctx, &event)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, delivery)
	assert.Less(t, time.Since(start), 2*time.Second, "the handshake stops at cancel, not at the 30s connection timeout")
}

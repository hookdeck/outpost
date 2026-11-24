package destkafka_test

import (
	"context"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destkafka"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const fakeTopic = "test-topic"

// fakeBroker speaks enough of the protocol for a producer to connect,
// authenticate with SASL plain and load the metadata of fakeTopic.
type fakeBroker struct {
	addr string
	// Address advertised as the leader of fakeTopic's only partition.
	leaderHost string
	leaderPort int32
	// When set, Metadata replies wait until it is closed.
	metadataGate chan struct{}
	produces     atomic.Int32
}

func (b *fakeBroker) serve(c net.Conn) {
	defer c.Close()
	for {
		r, err := readFakeRequest(c)
		if err != nil {
			return
		}
		var reply []byte
		switch r.key {
		case 18:
			reply = fakeAPIVersions(r)
		case 17:
			resp := kmsg.NewSASLHandshakeResponse()
			resp.Version = r.version
			resp.SupportedMechanisms = []string{"PLAIN"}
			reply = fakeFrame(r.correlation, resp.AppendTo(nil))
		case 36:
			resp := kmsg.NewSASLAuthenticateResponse()
			resp.Version = r.version
			reply = fakeFrame(r.correlation, resp.AppendTo(nil))
		case 3:
			if b.metadataGate != nil {
				<-b.metadataGate
			}
			reply = fakeFrame(r.correlation, b.metadata(r).AppendTo(nil))
		case 0:
			// No reply: the producer sends with acks off.
			b.produces.Add(1)
			continue
		default:
			return
		}
		if _, err := c.Write(reply); err != nil {
			return
		}
	}
}

func (b *fakeBroker) metadata(r fakeRequest) *kmsg.MetadataResponse {
	resp := kmsg.NewMetadataResponse()
	resp.Version = r.version
	resp.ControllerID = 1

	broker := kmsg.NewMetadataResponseBroker()
	broker.NodeID = 1
	broker.Host = b.leaderHost
	broker.Port = b.leaderPort
	resp.Brokers = append(resp.Brokers, broker)

	partition := kmsg.NewMetadataResponseTopicPartition()
	partition.Leader = 1
	partition.Replicas = []int32{1}
	partition.ISR = []int32{1}
	topic := kmsg.NewMetadataResponseTopic()
	topic.Topic = kmsg.StringPtr(fakeTopic)
	topic.Partitions = append(topic.Partitions, partition)
	resp.Topics = append(resp.Topics, topic)
	return &resp
}

// startFakeBroker advertises itself as the leader; change leaderHost and
// leaderPort before the first publish to advertise another address.
func startFakeBroker(t *testing.T) *fakeBroker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	host, port := splitFakeAddr(t, ln.Addr().String())
	b := &fakeBroker{addr: ln.Addr().String(), leaderHost: host, leaderPort: port}

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
				b.serve(c)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return b
}

func splitFakeAddr(t *testing.T, addr string) (string, int32) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return host, int32(port)
}

func newFakeBrokerPublisher(t *testing.T, addr string) destregistry.Publisher {
	t.Helper()
	provider, err := destkafka.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("kafka"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"brokers":        addr,
			"topic":          fakeTopic,
			"sasl_mechanism": "plain",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": "user",
			"password": "pass",
		}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	t.Cleanup(func() { _ = publisher.Close() })
	return publisher
}

// A leader that refuses connections fails the attempt with connection_refused,
// not the generic code of kgo's retries error.
func TestKafkaPublisher_LeaderDialRefused(t *testing.T) {
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedAddr := closed.Addr().String()
	require.NoError(t, closed.Close())

	broker := startFakeBroker(t)
	broker.leaderHost, broker.leaderPort = splitFakeAddr(t, closedAddr)
	publisher := newFakeBrokerPublisher(t, broker.addr)

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	delivery, err := publisher.Publish(ctx, &event)

	require.Error(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "failed", delivery.Status)
	assert.Equal(t, "connection_refused", delivery.Code)
	assert.Zero(t, broker.produces.Load())
}

// A publish that times out is dropped by the client: nothing is written once
// the attempt has been reported as failed, even if the broker recovers.
func TestKafkaPublisher_NoWriteAfterTimeout(t *testing.T) {
	broker := startFakeBroker(t)
	broker.metadataGate = make(chan struct{})
	publisher := newFakeBrokerPublisher(t, broker.addr)

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	delivery, err := publisher.Publish(ctx, &event)
	require.Error(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, "timeout", delivery.Code)

	// The broker recovers while the publisher is still open.
	close(broker.metadataGate)
	time.Sleep(1500 * time.Millisecond)
	assert.Zero(t, broker.produces.Load(), "a write landed after the attempt failed")

	// The same publisher still delivers.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	_, err = publisher.Publish(ctx2, &event)
	require.NoError(t, err)
	assert.Eventually(t, func() bool { return broker.produces.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
}

package destregistrydefault_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawseventbridge"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawssqs"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destgcppubsub"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func cloudProvider(t *testing.T, kind string, policy *destregistry.CloudEndpointPolicy) destregistry.Provider {
	t.Helper()
	loader := metadata.NewMetadataLoader("")
	var p destregistry.Provider
	var err error
	switch kind {
	case "aws_eventbridge":
		p, err = destawseventbridge.New(loader, nil, destawseventbridge.WithSource("outpost"), destawseventbridge.WithEndpointPolicy(policy))
	case "aws_kinesis":
		p, err = destawskinesis.New(loader, nil, destawskinesis.WithEndpointPolicy(policy))
	case "aws_sqs":
		p, err = destawssqs.New(loader, nil, destawssqs.WithEndpointPolicy(policy))
	case "aws_s3":
		p, err = destawss3.New(loader, nil, destawss3.WithEndpointPolicy(policy))
	case "gcp_pubsub":
		p, err = destgcppubsub.New(loader, nil, destgcppubsub.WithEndpointPolicy(policy))
	}
	require.NoError(t, err)
	return p
}

func cloudDestination(kind, endpoint string) *models.Destination {
	if kind == "gcp_pubsub" {
		endpoint = strings.TrimPrefix(endpoint, "http://")
	}
	return &models.Destination{ID: "test-destination", TenantID: "test-tenant", Type: kind,
		Config:      models.Config{"endpoint": endpoint, "region": "us-east-1", "event_bus_name": "default", "stream_name": "test-stream", "bucket": "test-bucket", "queue_url": "https://sqs.us-east-1.amazonaws.com/123456789012/test", "project_id": "test-project", "topic": "test-topic", "cloud_allowed_cidrs": "0.0.0.0/0"},
		Credentials: models.Credentials{"key": "fake-key", "secret": "fake-secret", "service_account_json": "{}"},
	}
}

func TestCloudProvidersBlockPrivateEndpoints(t *testing.T) {
	for _, kind := range []string{"aws_eventbridge", "aws_kinesis", "aws_sqs", "aws_s3", "gcp_pubsub"} {
		t.Run(kind, func(t *testing.T) {
			p := cloudProvider(t, kind, nil)
			for _, host := range []string{"127.0.0.1", "169.254.169.254", "10.0.0.1", "[::1]", "[::ffff:127.0.0.1]"} {
				d := cloudDestination(kind, "http://"+host+":8085")
				require.Error(t, p.Validate(context.Background(), d), host)
				// Existing records must fail too; API validation is not the only control.
				pub, err := p.CreatePublisher(context.Background(), d)
				require.Error(t, err, host)
				require.Nil(t, pub)
			}
			require.NoError(t, p.Validate(context.Background(), cloudDestination(kind, "http://custom.example.com:8085")))
		})
	}
}

func TestSQSQueueURLCannotBypassEndpointPolicy(t *testing.T) {
	p := cloudProvider(t, "aws_sqs", nil)
	d := cloudDestination("aws_sqs", "")
	d.Config["queue_url"] = "http://127.0.0.1:4566/123456789012/test"
	require.Error(t, p.Validate(context.Background(), d))
	_, err := p.CreatePublisher(context.Background(), d)
	require.Error(t, err)
	// LocalStack can legitimately use different hosts in queue_url and endpoint.
	d.Config["endpoint"] = "https://custom.example.com"
	require.NoError(t, p.Validate(context.Background(), d))
}

func TestCloudProvidersEnforceResolvedAddressAndPreserveEmulators(t *testing.T) {
	allowed, err := destregistry.NewCloudEndpointPolicy("127.0.0.1/32,::1/128")
	require.NoError(t, err)
	for _, kind := range []string{"aws_eventbridge", "aws_kinesis", "aws_sqs", "aws_s3"} {
		t.Run(kind, func(t *testing.T) {
			var hits atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				require.Contains(t, r.Header.Get("Authorization"), "Credential=fake-key/")
				w.Header().Set("Content-Type", "application/x-amz-json-1.1")
				switch kind {
				case "aws_eventbridge":
					io.WriteString(w, `{"FailedEntryCount":0,"Entries":[{"EventId":"test-id"}]}`)
				case "aws_kinesis":
					io.WriteString(w, `{"ShardId":"test-shard","SequenceNumber":"1"}`)
				case "aws_sqs":
					io.WriteString(w, `{"MessageId":"test-id"}`)
				}
			}))
			defer s.Close()
			d := cloudDestination(kind, strings.Replace(s.URL, "127.0.0.1", "localhost", 1))
			event := &models.Event{ID: "test-event", TenantID: "test-tenant", Topic: "test.event", Time: time.Now(), Data: models.Data(`{"test":true}`)}
			p := cloudProvider(t, kind, nil)
			require.NoError(t, p.Validate(context.Background(), d))
			pub, err := p.CreatePublisher(context.Background(), d)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			_, err = pub.Publish(ctx, event)
			cancel()
			require.Error(t, err)
			require.NoError(t, pub.Close())
			require.Zero(t, hits.Load())
			pub, err = cloudProvider(t, kind, allowed).CreatePublisher(context.Background(), d)
			require.NoError(t, err)
			defer pub.Close()
			ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			delivery, err := pub.Publish(ctx, event)
			require.NoError(t, err)
			require.Equal(t, "success", delivery.Status)
			require.EqualValues(t, 1, hits.Load())
		})
	}
}

func TestCloudEndpointPolicyRegistration(t *testing.T) {
	for _, cidrs := range []string{"", "127.0.0.1/32", "not-a-cidr"} {
		registry := destregistry.NewRegistry(&destregistry.Config{}, logging.NewTestLogger(zap.NewNop()))
		cfg := &config.Config{}
		cfg.InitDefaults()
		cfg.Destinations.CloudAllowedCIDRs = cidrs
		err := destregistrydefault.RegisterDefault(registry, cfg.Destinations.ToConfig(cfg))
		if cidrs == "not-a-cidr" {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		for _, kind := range []string{"aws_eventbridge", "aws_kinesis", "aws_sqs", "aws_s3", "gcp_pubsub"} {
			err := registry.ValidateDestination(context.Background(), cloudDestination(kind, "http://127.0.0.1:8085"))
			if cidrs == "" {
				require.Error(t, err, kind)
			} else {
				require.NoError(t, err, kind)
			}
		}
	}
}

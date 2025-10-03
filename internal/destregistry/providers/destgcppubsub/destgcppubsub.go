package destgcppubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GCPPubSubDestination struct {
	*destregistry.BaseProvider
}

type GCPPubSubDestinationConfig struct {
	ProjectID string
	Topic     string
	Endpoint  string // For emulator support
}

type GCPPubSubDestinationCredentials struct {
	ServiceAccountJSON string
}

var _ destregistry.Provider = (*GCPPubSubDestination)(nil)

func New(loader metadata.MetadataLoader) (*GCPPubSubDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "gcp_pubsub")
	if err != nil {
		return nil, err
	}

	return &GCPPubSubDestination{
		BaseProvider: base,
	}, nil
}

func (d *GCPPubSubDestination) Validate(ctx context.Context, destination *models.Destination) error {
	_, _, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return err
	}
	return nil
}

func (d *GCPPubSubDestination) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	cfg, creds, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return nil, err
	}

	// Create Pub/Sub client options
	var opts []option.ClientOption

	// Check for emulator endpoint (for testing)
	if cfg.Endpoint != "" {
		opts = append(opts,
			option.WithEndpoint(cfg.Endpoint),
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)
	} else if creds.ServiceAccountJSON != "" {
		// Use service account credentials for production
		opts = append(opts, option.WithCredentialsJSON([]byte(creds.ServiceAccountJSON)))
	}

	// Create the client
	client, err := pubsub.NewClient(ctx, cfg.ProjectID, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create pubsub client: %w", err)
	}

	// Get the topic
	topic := client.Topic(cfg.Topic)

	return &GCPPubSubPublisher{
		BasePublisher: &destregistry.BasePublisher{},
		client:        client,
		topic:         topic,
		projectID:     cfg.ProjectID,
	}, nil
}

func (d *GCPPubSubDestination) resolveMetadata(ctx context.Context, destination *models.Destination) (*GCPPubSubDestinationConfig, *GCPPubSubDestinationCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	return &GCPPubSubDestinationConfig{
			ProjectID: destination.Config["project_id"],
			Topic:     destination.Config["topic"],
			Endpoint:  destination.Config["endpoint"], // For testing
		}, &GCPPubSubDestinationCredentials{
			ServiceAccountJSON: destination.Credentials["service_account_json"],
		}, nil
}

func (d *GCPPubSubDestination) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	projectID := destination.Config["project_id"]
	topic := destination.Config["topic"]

	return destregistry.DestinationTarget{
		Target:    fmt.Sprintf("%s/%s", projectID, topic),
		TargetURL: fmt.Sprintf("https://console.cloud.google.com/cloudpubsub/topic/detail/%s?project=%s", topic, projectID),
	}
}

type GCPPubSubPublisher struct {
	*destregistry.BasePublisher

	client    *pubsub.Client
	topic     *pubsub.Topic
	projectID string
	mu        sync.Mutex
}

func (pub *GCPPubSubPublisher) Format(ctx context.Context, event *models.Event) (*pubsub.Message, error) {
	// Marshal event data to JSON
	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}

	// Create metadata
	metadata := pub.BasePublisher.MakeMetadata(event, time.Now())

	// Convert metadata to Pub/Sub attributes (must be strings)
	attributes := make(map[string]string)
	for k, v := range metadata {
		attributes[k] = v
	}

	return &pubsub.Message{
		Data:       dataBytes,
		Attributes: attributes,
	}, nil
}

func (pub *GCPPubSubPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if err := pub.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer pub.BasePublisher.FinishPublish()

	// Format the message
	msg, err := pub.Format(ctx, event)
	if err != nil {
		return nil, err
	}

	// Publish the message
	result := pub.topic.Publish(ctx, msg)

	// Wait for the publish to complete
	messageID, err := result.Get(ctx)
	if err != nil {
		return &destregistry.Delivery{
				Status: "failed",
				Code:   "ERR",
				Response: map[string]interface{}{
					"error": err.Error(),
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "gcp_pubsub", map[string]interface{}{
				"error":   "publish_failed",
				"project": pub.projectID,
				"topic":   pub.topic.ID(),
				"message": err.Error(),
			})
	}

	return &destregistry.Delivery{
		Status: "success",
		Code:   "OK",
		Response: map[string]interface{}{
			"message_id": messageID,
			"topic":      pub.topic.ID(),
			"project":    pub.projectID,
		},
	}, nil
}

func (pub *GCPPubSubPublisher) Close() error {
	pub.BasePublisher.StartClose()

	pub.mu.Lock()
	defer pub.mu.Unlock()

	if pub.topic != nil {
		pub.topic.Stop()
	}
	if pub.client != nil {
		return pub.client.Close()
	}

	return nil
}

package aws

import (
	"context"
	"encoding/json"
	"errors"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	"github.com/hookdeck/EventKit/internal/ingest"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/awssnssqs"
)

type AWSDestination struct {
}

type AWSDestinationConfig struct {
	Endpoint string // optional
	QueueURL string
}

var _ adapters.DestinationAdapter = (*AWSDestination)(nil)

func New() *AWSDestination {
	return &AWSDestination{}
}

func (d *AWSDestination) Validate(ctx context.Context, destination adapters.DestinationAdapterValue) error {
	_, err := parseConfig(destination)
	return err
}

func (d *AWSDestination) Publish(ctx context.Context, destination adapters.DestinationAdapterValue, event *ingest.Event) error {
	config, err := parseConfig(destination)
	if err != nil {
		return err
	}
	return publishEvent(ctx, config, event)
}

func parseConfig(destination adapters.DestinationAdapterValue) (*AWSDestinationConfig, error) {
	if destination.Type != "aws" {
		return nil, errors.New("invalid destination type")
	}

	destinationConfig := &AWSDestinationConfig{
		Endpoint: destination.Config["endpoint"],
		QueueURL: destination.Config["queue_url"],
	}

	if destinationConfig.QueueURL == "" {
		return nil, errors.New("queue_url is required for aws destination config")
	}

	return destinationConfig, nil
}

func publishEvent(ctx context.Context, cfg *AWSDestinationConfig, event *ingest.Event) error {
	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}

	sdkConfig, err := config.LoadDefaultConfig(ctx,
		// TODO: use proper credentials
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		return err
	}

	sqsClient := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = awssdk.String(cfg.Endpoint)
		}
	})

	topic := awssnssqs.OpenSQSTopicV2(ctx, sqsClient, cfg.QueueURL, nil)
	defer topic.Shutdown(ctx)

	return topic.Send(ctx, &pubsub.Message{
		Body: dataBytes,
	})
}

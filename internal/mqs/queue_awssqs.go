package mqs

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go/aws"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/awssnssqs"
)

type AWSSQSConfig struct {
	Endpoint                  string // optional - dev-focused
	Region                    string
	ServiceAccountCredentials string
	Topic                     string
}

func (c *AWSSQSConfig) ToCredentials() (*credentials.StaticCredentialsProvider, error) {
	creds := strings.Split(c.ServiceAccountCredentials, ":")
	if len(creds) != 3 {
		return nil, errors.New("invalid AWS Service Account Credentials")
	}
	awsCreds := credentials.NewStaticCredentialsProvider(creds[0], creds[1], creds[2])
	return &awsCreds, nil
}

type AWSQueue struct {
	once        *sync.Once
	base        *wrappedBaseQueue
	sqsQueueURL string
	sqsClient   *sqs.Client
	config      *AWSSQSConfig
	topic       *pubsub.Topic
}

var _ Queue = &AWSQueue{}

func NewAWSQueue(config *AWSSQSConfig) *AWSQueue {
	var once sync.Once
	return &AWSQueue{config: config, once: &once, base: newWrappedBaseQueue()}
}

func (q *AWSQueue) Init(ctx context.Context) (func(), error) {
	var err error
	q.once.Do(func() {
		err = q.InitSDK(ctx)
	})
	if err != nil {
		return nil, err
	}
	q.topic = awssnssqs.OpenSQSTopicV2(ctx, q.sqsClient, q.sqsQueueURL, nil)
	return func() {
		q.topic.Shutdown(ctx)
	}, nil
}

func (q *AWSQueue) Publish(ctx context.Context, incomingMessage IncomingMessage) error {
	return q.base.Publish(ctx, q.topic, incomingMessage, nil)
}

func (q *AWSQueue) Subscribe(ctx context.Context) (Subscription, error) {
	var err error
	q.once.Do(func() {
		err = q.InitSDK(ctx)
	})
	if err != nil {
		return nil, err
	}
	subscription := awssnssqs.OpenSubscriptionV2(ctx, q.sqsClient, q.sqsQueueURL, nil)
	return q.base.Subscribe(ctx, subscription)
}

func (q *AWSQueue) InitSDK(ctx context.Context) error {
	creds, err := q.config.ToCredentials()
	if err != nil {
		return err
	}

	sdkConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(q.config.Region),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return err
	}

	sqsClient := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		if q.config.Endpoint != "" {
			o.BaseEndpoint = aws.String(q.config.Endpoint)
		}
	})
	q.sqsClient = sqsClient

	queue, err := q.sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(q.config.Topic),
	})
	if err != nil {
		return err
	}
	q.sqsQueueURL = *queue.QueueUrl

	return nil
}

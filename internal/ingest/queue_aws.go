package ingest

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/smithy-go"
	"github.com/spf13/viper"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/awssnssqs"
)

// ============================== Config ==============================

type AWSSQSConfig struct {
	Endpoint                  string // optional - dev-focused
	Region                    string
	ServiceAccountCredentials string
	PublishTopic              string
}

func (c *IngestConfig) parseAWSSQSConfig(viper *viper.Viper) {
	if !viper.IsSet("AWS_SQS_SERVICE_ACCOUNT_CREDS") {
		return
	}

	config := &AWSSQSConfig{}
	config.Endpoint = viper.GetString("AWS_SQS_ENDPOINT")
	config.Region = viper.GetString("AWS_SQS_REGION")
	config.ServiceAccountCredentials = viper.GetString("AWS_SQS_SERVICE_ACCOUNT_CREDS")
	config.PublishTopic = viper.GetString("AWS_SQS_PUBLISH_TOPIC")

	c.AWSSQS = config
}

func (c *IngestConfig) validateAWSSQSConfig() error {
	if c.AWSSQS == nil {
		return nil
	}

	if c.AWSSQS.ServiceAccountCredentials == "" {
		return errors.New("AWS SQS Service Account Credentials is not set")
	} else {
		creds := strings.Split(c.AWSSQS.ServiceAccountCredentials, ":")
		if len(creds) != 3 {
			return errors.New("Invalid AWS Service Account Credentials")
		}
	}

	if c.AWSSQS.Region == "" {
		return errors.New("AWS SQS Region is not set")
	}

	if c.AWSSQS.PublishTopic == "" {
		return errors.New("AWS SQS Publish Topic is not set")
	}

	return nil
}

func (c *AWSSQSConfig) toCredentials() (*credentials.StaticCredentialsProvider, error) {
	creds := strings.Split(c.ServiceAccountCredentials, ":")
	if len(creds) != 3 {
		return nil, errors.New("Invalid AWS Service Account Credentials")
	}
	awsCreds := credentials.NewStaticCredentialsProvider(creds[0], creds[1], creds[2])
	return &awsCreds, nil
}

// ============================== Queue ==============================

type AWSQueue struct {
	sqsQueueURL string
	sqsClient   *sqs.Client
	config      *AWSSQSConfig
	topic       *pubsub.Topic
}

var _ IngestQueue = &AWSQueue{}

func NewAWSQueue(config *AWSSQSConfig) *AWSQueue {
	return &AWSQueue{config: config}
}

func (q *AWSQueue) Init(ctx context.Context) (func(), error) {
	err := q.init(ctx)
	if err != nil {
		return nil, err
	}
	q.topic = awssnssqs.OpenSQSTopicV2(ctx, q.sqsClient, q.sqsQueueURL, nil)
	return func() {
		q.topic.Shutdown(ctx)
	}, nil
}

func (q *AWSQueue) Publish(ctx context.Context, event Event) error {
	msg, err := event.ToMessage()
	if err != nil {
		return err
	}
	return q.topic.Send(ctx, msg)
}

func (q *AWSQueue) Subscribe(ctx context.Context) (Subscription, error) {
	subscription := awssnssqs.OpenSubscriptionV2(ctx, q.sqsClient, q.sqsQueueURL, nil)
	return wrappedSubscription(subscription)
}

func (q *AWSQueue) init(ctx context.Context) error {
	creds, err := q.config.toCredentials()
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

	queueURL, err := ensureQueue(ctx, sqsClient, q.config.PublishTopic)
	if err != nil {
		return err
	}
	q.sqsQueueURL = queueURL

	return nil
}

func ensureQueue(ctx context.Context, sqsClient *sqs.Client, queueName string) (string, error) {
	queue, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.(type) {
			case *types.QueueDoesNotExist:
				log.Println("Queue does not exist, creating...")
				createdQueue, err := sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{
					QueueName: aws.String(queueName),
				})
				if err != nil {
					return "", err
				}
				return *createdQueue.QueueUrl, nil
			default:
				return "", err
			}
		}
		return "", err
	}
	return *queue.QueueUrl, nil
}

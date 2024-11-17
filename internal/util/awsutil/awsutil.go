package awsutil

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/smithy-go"
	"github.com/hookdeck/outpost/internal/mqs"
)

func SQSClientFromConfig(ctx context.Context, cfg *mqs.AWSSQSConfig) (*sqs.Client, error) {
	creds, err := cfg.ToCredentials()
	if err != nil {
		return nil, err
	}

	sdkConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return nil, err
	}

	sqsClient := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})
	return sqsClient, nil
}

type CreateQueueFn func(ctx context.Context, sqsClient *sqs.Client, queueName string) (*sqs.CreateQueueOutput, error)

func EnsureQueue(ctx context.Context, sqsClient *sqs.Client, queueName string, createQueue CreateQueueFn) (string, error) {
	queue, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.(type) {
			case *types.QueueDoesNotExist:
				createdQueue, err := createQueue(ctx, sqsClient, queueName)
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

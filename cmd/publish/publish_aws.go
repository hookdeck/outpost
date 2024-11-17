package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/awsutil"
)

const (
	AWSRegion        = "eu-central-1"
	AWSEndpoint      = "http://localhost:4566"
	AWSCredentials   = "test:test:"
	PublishQueueName = "publish_sqs_queue"
)

var awsConfig = &mqs.AWSSQSConfig{
	Region:                    AWSRegion,
	Endpoint:                  AWSEndpoint,
	ServiceAccountCredentials: AWSCredentials,
	Topic:                     PublishQueueName,
}

func publishAWS(body map[string]interface{}) error {
	log.Printf("[x] Publishing AWS")

	ctx := context.Background()
	sqsClient, err := awsutil.SQSClientFromConfig(ctx, awsConfig)
	if err != nil {
		return err
	}

	queueURL, err := getQueue(ctx, sqsClient, PublishQueueName)
	if err != nil {
		return err
	}

	messageBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(messageBody)),
	})

	return nil
}

func declareAWS() error {
	log.Printf("[*] Declaring AWS Publish infra")
	ctx := context.Background()
	sqsClient, err := awsutil.SQSClientFromConfig(ctx, awsConfig)
	if err != nil {
		return err
	}
	_, err = awsutil.EnsureQueue(ctx, sqsClient, PublishQueueName, createQueue)
	return err
}

func getQueue(ctx context.Context, sqsClient *sqs.Client, queueName string) (string, error) {
	queue, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		return "", err
	}
	return *queue.QueueUrl, nil
}

func createQueue(ctx context.Context, sqsClient *sqs.Client, queueName string) (*sqs.CreateQueueOutput, error) {
	return sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(queueName),
	})
}

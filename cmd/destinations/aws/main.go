package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/awsutil"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

const (
	AWSRegion            = "eu-central-1"
	AWSEndpoint          = "http://localhost:4566"
	AWSCredentials       = "test:test:"
	DestinationQueueName = "destination_sqs_queue"
)

func run() error {
	ctx := context.Background()

	awsConfig := &mqs.AWSSQSConfig{
		Region:                    AWSRegion,
		Endpoint:                  AWSEndpoint,
		ServiceAccountCredentials: AWSCredentials,
		Topic:                     DestinationQueueName,
	}

	sqsClient, err := awsutil.SQSClientFromConfig(ctx, awsConfig)
	if err != nil {
		return err
	}

	queueURL, err := awsutil.EnsureQueue(ctx, sqsClient, DestinationQueueName, awsutil.MakeCreateQueue(nil))
	if err != nil {
		return err
	}

	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			out, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl:              &queueURL,
				MaxNumberOfMessages:   1,
				VisibilityTimeout:     3,
				WaitTimeSeconds:       10,
				MessageAttributeNames: []string{"All"},
			})
			if err != nil {
				log.Printf("[*] error on recv: %v", err)
				continue
			}
			if len(out.Messages) == 0 {
				continue
			}
			for _, m := range out.Messages {
				log.Printf("[x] %s - %s\n", string(*m.Body), *m.MessageId)

				// Delete message (to ack)
				_, err = sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
					QueueUrl:      &queueURL,
					ReceiptHandle: m.ReceiptHandle,
				})
				if err != nil {
					log.Printf("[x] error deleting message %s: %v", *m.MessageId, err)
					continue
				}
			}
		}
	}()

	log.Printf("[*] Ready to receive messages.\n\tEndpoint: %s\n\tQueue: %s", AWSEndpoint, queueURL)
	log.Printf("[*] Waiting for logs. To exit press CTRL+C")
	<-termChan

	return nil
}

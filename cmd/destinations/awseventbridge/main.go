package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

const (
	awsRegion      = "us-east-1"
	awsEndpoint    = "http://localhost:24566"
	awsAccessKey   = "test"
	awsSecretKey   = "test"
	eventBusName   = "destination_eventbridge_bus"
	ruleName       = "destination_eventbridge_rule"
	targetID       = "sqs"
	targetQueue    = "destination_eventbridge_queue"
	matchAllEvents = `{"source":[{"prefix":""}]}`
)

func main() {
	ctx := context.Background()

	awsConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(awsRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(awsAccessKey, awsSecretKey, "")),
	)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}
	ebClient := eventbridge.NewFromConfig(awsConfig, func(o *eventbridge.Options) {
		o.BaseEndpoint = aws.String(awsEndpoint)
	})
	sqsClient := sqs.NewFromConfig(awsConfig, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(awsEndpoint)
	})

	switch {
	case len(os.Args) == 1:
	case len(os.Args) == 2 && os.Args[1] == "down":
		teardown(ctx, ebClient, sqsClient)
		return
	default:
		usage()
		os.Exit(2)
	}

	queueURL, err := setup(ctx, ebClient, sqsClient)
	if err != nil {
		log.Fatalf("Failed to set up the event bus: %v", err)
	}

	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	go consume(ctx, sqsClient, queueURL)

	log.Printf("[*] Ready to receive events from EventBridge")
	log.Printf("[*] Configuration:")
	log.Printf("\tEndpoint: %s (use 'http://localstack:4566' if Outpost runs inside the Docker network)", awsEndpoint)
	log.Printf("\tRegion: %s", awsRegion)
	log.Printf("\tEvent bus: %s", eventBusName)
	log.Printf("\tCredentials: %s / %s", awsAccessKey, awsSecretKey)
	log.Printf("[*] Every event on the bus is forwarded to the SQS queue %s and printed here", targetQueue)
	usage()
	log.Printf("[*] Waiting for logs. To exit press CTRL+C")
	<-termChan
}

func usage() {
	log.Printf("[*] Available commands:")
	log.Printf("\tgo run ./cmd/destinations/awseventbridge        - Start consumer")
	log.Printf("\tgo run ./cmd/destinations/awseventbridge down   - Delete the bus, rule and queue")
}

// setup creates the event bus, a queue, and a rule that forwards every event
// on the bus to the queue. It returns the queue URL.
func setup(ctx context.Context, ebClient *eventbridge.Client, sqsClient *sqs.Client) (string, error) {
	queue, err := sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(targetQueue)})
	if err != nil {
		return "", fmt.Errorf("create queue: %w", err)
	}
	attrs, err := sqsClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       queue.QueueUrl,
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return "", fmt.Errorf("get queue ARN: %w", err)
	}

	if _, err := ebClient.DescribeEventBus(ctx, &eventbridge.DescribeEventBusInput{Name: aws.String(eventBusName)}); err != nil {
		log.Printf("[*] Creating event bus %s...", eventBusName)
		if _, err := ebClient.CreateEventBus(ctx, &eventbridge.CreateEventBusInput{Name: aws.String(eventBusName)}); err != nil {
			return "", fmt.Errorf("create event bus: %w", err)
		}
	} else {
		log.Printf("[*] Event bus %s already exists", eventBusName)
	}

	if _, err := ebClient.PutRule(ctx, &eventbridge.PutRuleInput{
		Name:         aws.String(ruleName),
		EventBusName: aws.String(eventBusName),
		EventPattern: aws.String(matchAllEvents),
	}); err != nil {
		return "", fmt.Errorf("put rule: %w", err)
	}
	if _, err := ebClient.PutTargets(ctx, &eventbridge.PutTargetsInput{
		Rule:         aws.String(ruleName),
		EventBusName: aws.String(eventBusName),
		Targets:      []ebtypes.Target{{Id: aws.String(targetID), Arn: aws.String(attrs.Attributes["QueueArn"])}},
	}); err != nil {
		return "", fmt.Errorf("put targets: %w", err)
	}

	return aws.ToString(queue.QueueUrl), nil
}

func teardown(ctx context.Context, ebClient *eventbridge.Client, sqsClient *sqs.Client) {
	report := func(what string, err error) {
		if err != nil {
			log.Printf("[*] Could not delete %s: %v", what, err)
			return
		}
		log.Printf("[*] Deleted %s", what)
	}

	_, err := ebClient.RemoveTargets(ctx, &eventbridge.RemoveTargetsInput{
		Rule:         aws.String(ruleName),
		EventBusName: aws.String(eventBusName),
		Ids:          []string{targetID},
	})
	if err == nil {
		_, err = ebClient.DeleteRule(ctx, &eventbridge.DeleteRuleInput{
			Name:         aws.String(ruleName),
			EventBusName: aws.String(eventBusName),
		})
	}
	report("rule "+ruleName, err)

	_, err = ebClient.DescribeEventBus(ctx, &eventbridge.DescribeEventBusInput{Name: aws.String(eventBusName)})
	if err == nil {
		_, err = ebClient.DeleteEventBus(ctx, &eventbridge.DeleteEventBusInput{Name: aws.String(eventBusName)})
	}
	report("event bus "+eventBusName, err)

	queue, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(targetQueue)})
	if err == nil {
		_, err = sqsClient.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: queue.QueueUrl})
	}
	report("queue "+targetQueue, err)
}

func consume(ctx context.Context, sqsClient *sqs.Client, queueURL string) {
	for {
		out, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     10,
		})
		if err != nil {
			log.Printf("[*] error on recv: %v", err)
			time.Sleep(time.Second)
			continue
		}
		for _, m := range out.Messages {
			log.Printf("[x] %s", aws.ToString(m.Body))
			if _, err := sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(queueURL),
				ReceiptHandle: m.ReceiptHandle,
			}); err != nil {
				log.Printf("[x] error deleting message %s: %v", aws.ToString(m.MessageId), err)
			}
		}
	}
}

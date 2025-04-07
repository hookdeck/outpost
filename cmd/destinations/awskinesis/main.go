package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
)

const (
	AWSRegion             = "us-east-1"
	AWSEndpoint           = "http://localhost:4566"
	AWSCredentials        = "test:test:"
	DestinationStreamName = "destination_kinesis_stream"
	ShardCount            = 1
)

func main() {
	ctx := context.Background()

	// Set up AWS configuration and client
	kinesisClient, err := setupKinesisClient(ctx)
	if err != nil {
		log.Fatalf("Failed to set up Kinesis client: %v", err)
	}

	// Check for command line arguments
	if len(os.Args) > 1 && os.Args[1] == "down" {
		if err := deleteStream(ctx, kinesisClient); err != nil {
			log.Fatalf("Error deleting stream: %v", err)
		}
		return
	}

	if err := consumeMessages(ctx, kinesisClient); err != nil {
		log.Fatalf("Error consuming messages: %v", err)
	}
}

// setupKinesisClient creates and configures a Kinesis client
func setupKinesisClient(ctx context.Context) (*kinesis.Client, error) {
	// Configure AWS SDK
	awsConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(AWSRegion),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				AWSCredentials[:len(AWSCredentials)-1], // remove the trailing colon
				AWSCredentials[len(AWSCredentials)-1:], // just get the empty string after colon
				""),
		),
	)
	if err != nil {
		return nil, err
	}

	// Create Kinesis client with custom endpoint
	kinesisClient := kinesis.NewFromConfig(awsConfig, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(AWSEndpoint)
	})

	return kinesisClient, nil
}

// deleteStream deletes the Kinesis stream
func deleteStream(ctx context.Context, client *kinesis.Client) error {
	// Check if stream exists before attempting to delete
	_, err := client.DescribeStream(ctx, &kinesis.DescribeStreamInput{
		StreamName: aws.String(DestinationStreamName),
	})

	if err != nil {
		log.Printf("[*] Stream %s does not exist or cannot be accessed, nothing to clean up", DestinationStreamName)
		return nil
	}

	// Delete the stream
	log.Printf("[*] Deleting stream %s...", DestinationStreamName)
	_, err = client.DeleteStream(ctx, &kinesis.DeleteStreamInput{
		StreamName: aws.String(DestinationStreamName),
	})
	if err != nil {
		return err
	}

	log.Printf("[*] Stream %s delete request sent successfully", DestinationStreamName)

	// Wait for stream to be deleted
	log.Printf("[*] Waiting for stream %s to be deleted...", DestinationStreamName)
	waiter := kinesis.NewStreamNotExistsWaiter(client)
	err = waiter.Wait(ctx, &kinesis.DescribeStreamInput{
		StreamName: aws.String(DestinationStreamName),
	}, 30*time.Second)

	if err == nil {
		log.Printf("[*] Stream %s has been deleted", DestinationStreamName)
	}

	return err
}

// consumeMessages runs the Kinesis consumer
func consumeMessages(ctx context.Context, client *kinesis.Client) error {
	// Ensure stream exists
	if err := ensureKinesisStream(ctx, client, DestinationStreamName); err != nil {
		return err
	}

	// Get shard ID
	describeOutput, err := client.DescribeStream(ctx, &kinesis.DescribeStreamInput{
		StreamName: aws.String(DestinationStreamName),
	})
	if err != nil {
		return err
	}

	if len(describeOutput.StreamDescription.Shards) == 0 {
		log.Printf("[*] No shards found in stream %s", DestinationStreamName)
		return nil
	}

	shardId := *describeOutput.StreamDescription.Shards[0].ShardId
	log.Printf("[*] Using shard ID: %s", shardId)

	// Set up termination signal handler
	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	// Get shard iterator
	iteratorOutput, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{
		StreamName:        aws.String(DestinationStreamName),
		ShardId:           aws.String(shardId),
		ShardIteratorType: types.ShardIteratorTypeLatest,
	})
	if err != nil {
		return err
	}

	iterator := iteratorOutput.ShardIterator

	// Start consuming in a goroutine
	go func() {
		for {
			// Get records using the shard iterator
			recordsOutput, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{
				ShardIterator: iterator,
				Limit:         aws.Int32(25),
			})
			if err != nil {
				log.Printf("[*] Error getting records: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}

			// Process each record
			for _, record := range recordsOutput.Records {
				// Try to unmarshal as JSON to pretty print
				var payload map[string]interface{}
				if err := json.Unmarshal(record.Data, &payload); err != nil {
					// If not JSON, just print raw data
					log.Printf("[x] Sequence Number: %s | Partition Key: %s | Payload: %s",
						*record.SequenceNumber, *record.PartitionKey, string(record.Data))
				} else {
					// Pretty print the JSON payload
					formattedData, _ := json.MarshalIndent(payload, "", "  ")
					log.Printf("[x] Sequence Number: %s | Partition Key: %s | Payload: %s",
						*record.SequenceNumber, *record.PartitionKey, string(formattedData))
				}
			}

			// Update the iterator for the next call
			iterator = recordsOutput.NextShardIterator
			if iterator == nil {
				log.Printf("[*] End of shard reached")
				break
			}

			// If no records, sleep a bit to avoid hitting API limits
			if len(recordsOutput.Records) == 0 {
				time.Sleep(1 * time.Second)
			}
		}
	}()

	log.Printf("[*] Ready to receive messages from Kinesis")
	log.Printf("[*] Configuration:")
	log.Printf("\tEndpoint: %s (use 'aws:4566' if running in Docker)", AWSEndpoint)
	log.Printf("\tRegion: %s", AWSRegion)
	log.Printf("\tStream: %s", DestinationStreamName)
	log.Printf("[*] Consumer set to read only NEW messages that arrive after startup")
	log.Printf("[*] Available commands:")
	log.Printf("\tgo run ./cmd/destinations/awskinesis        - Start consumer")
	log.Printf("\tgo run ./cmd/destinations/awskinesis down   - Delete stream")
	log.Printf("[*] Waiting for logs. To exit press CTRL+C")
	<-termChan

	return nil
}

// Create or ensure Kinesis stream exists
func ensureKinesisStream(ctx context.Context, client *kinesis.Client, streamName string) error {
	// Check if stream exists
	_, err := client.DescribeStream(ctx, &kinesis.DescribeStreamInput{
		StreamName: aws.String(streamName),
	})
	if err == nil {
		log.Printf("[*] Stream %s already exists", streamName)
		return nil
	}

	// Create the stream
	log.Printf("[*] Creating stream %s with %d shard(s)...", streamName, ShardCount)
	_, err = client.CreateStream(ctx, &kinesis.CreateStreamInput{
		StreamName: aws.String(streamName),
		ShardCount: aws.Int32(ShardCount),
	})
	if err != nil {
		return err
	}

	// Wait for stream to become active
	log.Printf("[*] Waiting for stream %s to become active...", streamName)
	waiter := kinesis.NewStreamExistsWaiter(client)
	err = waiter.Wait(ctx, &kinesis.DescribeStreamInput{
		StreamName: aws.String(streamName),
	}, 30*time.Second)

	if err == nil {
		log.Printf("[*] Stream %s is now active", streamName)
	}

	return err
}

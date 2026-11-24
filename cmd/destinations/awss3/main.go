package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/hookdeck/outpost/cmd/destinations/internal/destenv"
)

// Defaults match the local destination stack (`make up/dest`). For AWS
// itself, set DEST_AWS_ENDPOINT="" and DEST_AWS_CREDENTIALS="key:secret:session".
var (
	S3Region       = destenv.Get("DEST_AWS_REGION", "us-east-1")
	AWSEndpoint    = destenv.Get("DEST_AWS_ENDPOINT", "http://localhost:14566")
	AWSCredentials = destenv.Get("DEST_AWS_CREDENTIALS", "test:test:")
	S3Bucket       = destenv.Get("DEST_S3_BUCKET", "destination-s3-bucket")
)

// This program monitors an AWS S3 bucket for new objects and logs them.
// Note that this is meant for demonstration purposes and shouldn't be
// used on large buckets or in production without proper error handling
// and optimizations. Listing objects in a bucket is slow. For large
// buckets, consider using S3 event notifications or a more efficient
// mechanism to track new objects.
func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	ctx := context.Background()

	credsParts := strings.Split(AWSCredentials, ":")
	if len(credsParts) == 2 {
		credsParts = append(credsParts, "")
	}
	if len(credsParts) != 3 {
		return fmt.Errorf("invalid AWS credentials format (key:secret[:session])")
	}

	// Set up AWS configuration with the provided credentials
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(S3Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			credsParts[0], credsParts[1], credsParts[2],
		)),
	)
	if err != nil {
		return err
	}

	// Create an S3 client using the loaded configuration
	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if AWSEndpoint != "" {
			o.BaseEndpoint = aws.String(AWSEndpoint)
			o.UsePathStyle = true // LocalStack
		}
	})

	// A local stack starts empty: create the bucket there. On AWS the bucket
	// is yours to create.
	if AWSEndpoint != "" {
		if _, err := s3Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(S3Bucket)}); err != nil {
			log.Printf("[*] create bucket %s: %v (continuing)", S3Bucket, err)
		}
	}

	// Listen for termination signals
	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	// Maintain a list of seen objects to avoid logging duplicates
	seen := make(map[string]struct{})

	// First pass to populate the seen map with existing objects
	log.Printf("[*] Initializing S3 bucket monitoring...\n\tBucket: %s", S3Bucket)
	if err := checkForNewObjects(ctx, s3Client, seen, false); err != nil {
		return err
	}

	// Check for new objects every 10 seconds
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Start a goroutine to periodically check for new objects
	// and listen for termination signals.
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := checkForNewObjects(ctx, s3Client, seen, true); err != nil {
					log.Printf("[*] error listing bucket: %v", err)
				}
			case <-termChan:
				return
			}
		}
	}()

	log.Printf("[*] Ready to monitor S3 bucket.\n\tBucket: %s", S3Bucket)
	log.Printf("[*] Waiting for new objects. To exit press CTRL+C")

	<-termChan

	return nil
}

// checkForNewObjects lists objects in the S3 bucket and logs new ones.
func checkForNewObjects(ctx context.Context, client *s3.Client, seen map[string]struct{}, logNew bool) error {
	var token *string
	for {
		out, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(S3Bucket),
			ContinuationToken: token,
		})
		if err != nil {
			return err
		}
		for _, obj := range out.Contents {
			key := aws.ToString(obj.Key)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				if logNew {
					log.Printf("[x] New object: %s (%d bytes)", key, obj.Size)
				}
			}
		}
		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}
	return nil
}

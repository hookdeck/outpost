package destawss3_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// S3Consumer implements testsuite.MessageConsumer
type S3Consumer struct {
	client    *s3.Client
	bucket    string
	msgChan   chan testsuite.Message
	done      chan struct{}
	seenKeys  map[string]bool
}

func NewS3Consumer(client *s3.Client, bucket string) *S3Consumer {
	c := &S3Consumer{
		client:   client,
		bucket:   bucket,
		msgChan:  make(chan testsuite.Message, 100),
		done:     make(chan struct{}),
		seenKeys: make(map[string]bool),
	}
	go c.consume()
	return c
}

func (c *S3Consumer) consume() {
	ticker := time.NewTicker(100 * time.Millisecond) // Poll S3 every 100ms
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.pollS3()
		}
	}
}

func (c *S3Consumer) pollS3() {
	ctx := context.Background()
	
	// List all objects
	result, err := c.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
	})
	if err != nil {
		return
	}

	for _, obj := range result.Contents {
		key := *obj.Key
		
		// Skip if we've already seen this object
		if c.seenKeys[key] {
			continue
		}
		c.seenKeys[key] = true

		// Get the object
		getResult, err := c.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			continue
		}

		// Read the body as bytes
		body := make([]byte, 0)
		buf := make([]byte, 1024)
		for {
			n, err := getResult.Body.Read(buf)
			if n > 0 {
				body = append(body, buf[:n]...)
			}
			if err != nil {
				break
			}
		}
		getResult.Body.Close()

		// Convert metadata to map[string]string
		metadata := make(map[string]string)
		for k, v := range getResult.Metadata {
			metadata[k] = v
		}

		c.msgChan <- testsuite.Message{
			Data:     body,
			Metadata: metadata,
			Raw: map[string]interface{}{
				"key":      key,
				"metadata": metadata,
			},
		}
	}
}

func (c *S3Consumer) Consume() <-chan testsuite.Message {
	return c.msgChan
}

func (c *S3Consumer) Close() error {
	close(c.done)
	close(c.msgChan)
	return nil
}

// S3Asserter implements testsuite.MessageAsserter
type S3Asserter struct{}

func (a *S3Asserter) AssertMessage(t testsuite.TestingT, msg testsuite.Message, event models.Event) {
	// 1. Assert event data matches using JSON comparison (handles int/float64 conversion)
	expectedJSON, err := json.Marshal(event.Data)
	assert.NoError(t, err, "should be able to marshal expected data")
	assert.JSONEq(t, string(expectedJSON), string(msg.Data), "event data should match")

	// 2. Assert system metadata is present
	metadata := msg.Metadata
	assert.NotEmpty(t, metadata["timestamp"], "timestamp should be present")
	assert.Equal(t, event.ID, metadata["event-id"], "event-id should match")
	assert.Equal(t, event.Topic, metadata["topic"], "topic should match")

	// 3. Assert event metadata is preserved
	for key, value := range event.Metadata {
		assert.Equal(t, value, metadata[key], fmt.Sprintf("event metadata %s should be preserved", key))
	}
}

// S3PublishSuite uses the shared test suite
type S3PublishSuite struct {
	testsuite.PublisherSuite
	consumer *S3Consumer
	client   *s3.Client
	bucket   string
}

func (s *S3PublishSuite) SetupSuite() {
	t := s.T()
	t.Cleanup(testinfra.Start(t))

	// Get LocalStack endpoint
	endpoint := testinfra.EnsureLocalStack()
	
	// Set AWS environment variables for LocalStack
	// The AWS SDK v2 will pick these up automatically
	os.Setenv("AWS_ENDPOINT_URL_S3", endpoint)
	os.Setenv("AWS_S3_ENDPOINT", endpoint)
	os.Setenv("AWS_ENDPOINT_URL", endpoint)
	t.Cleanup(func() {
		os.Unsetenv("AWS_ENDPOINT_URL_S3")
		os.Unsetenv("AWS_S3_ENDPOINT")
		os.Unsetenv("AWS_ENDPOINT_URL")
	})
	
	// Create S3 client for test consumer
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)
	
	s.client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true // Required for LocalStack
	})
	
	// Create a unique bucket for this test
	s.bucket = fmt.Sprintf("test-bucket-%s", uuid.New().String())
	_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	})
	require.NoError(t, err)
	
	// Create provider
	provider, err := destawss3.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)
	
	// Create destination configuration
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_s3"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"bucket":        s.bucket,
			"region":        "us-east-1",
			"key_template":  `join('/', ['test', time.rfc3339_nano, metadata."event-id", '.json'])`,
			"storage_class": "STANDARD",
			"endpoint":      endpoint, // Use LocalStack endpoint
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":    "test",
			"secret": "test",
		}),
	)
	
	// Create consumer
	consumer := NewS3Consumer(s.client, s.bucket)
	s.consumer = consumer
	
	// Initialize suite 
	s.InitSuite(testsuite.Config{
		Provider: provider,
		Dest:     &dest,
		Consumer: consumer,
		Asserter: &S3Asserter{},
	})
}

func (s *S3PublishSuite) TearDownSuite() {
	if s.consumer != nil {
		s.consumer.Close()
	}
	
	// Clean up bucket
	if s.client != nil && s.bucket != "" {
		ctx := context.Background()
		
		// Delete all objects first
		listResult, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(s.bucket),
		})
		if err == nil {
			for _, obj := range listResult.Contents {
				s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(s.bucket),
					Key:    obj.Key,
				})
			}
		}
		
		// Delete bucket
		s.client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(s.bucket),
		})
	}
}

func TestS3PublishIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	suite.Run(t, new(S3PublishSuite))
}
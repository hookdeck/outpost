package e2e_test

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/require"
)

// TestE2E_Regression_FormatErrorIsDeliveredAttempt is a standalone regression test
// for the production incident where an aws_s3 destination's key_template could not be
// evaluated for a given event (the template referenced metadata.operationId, which the
// event lacked). Before the fix, the per-event formatting failure returned a nil
// delivery, which the pipeline treated as a system error: it was nacked, never logged
// as an attempt, retried blindly by the message queue, and ultimately dead-lettered —
// paging us instead of surfacing as a normal delivery failure.
//
// aws_s3 is the only destination type that can force this: its key_template is JMESPath
// validated for syntax only at creation, so a template that is valid at creation can
// still fail at delivery for an event missing a referenced field. (Kafka/Kinesis use the
// same per-event template mechanism but swallow eval errors and fall back to event.ID;
// the other providers have no per-event formatting step that can fail.)
//
// This test asserts the FIXED behavior, end to end:
//  1. nothing is ever written to S3 (the failure is pre-delivery)
//  2. each attempt is RECORDED as a failed delivery carrying the format error
//  3. it retries on the normal schedule and EXHAUSTS its budget, then stops —
//     it is never requeued/dead-lettered (which would log zero attempts and loop)
func TestE2E_Regression_FormatErrorIsDeliveredAttempt(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}

	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()
	gin.SetMode(gin.TestMode)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// LocalStack S3: the bucket doubles as the "was anything actually delivered?" sink.
	endpoint := testinfra.EnsureLocalStack()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(t, err)
	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = true // required for LocalStack
		o.BaseEndpoint = aws.String(endpoint)
	})
	bucket := fmt.Sprintf("regr-format-%d", time.Now().UnixNano())
	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	// Isolated outpost instance with a short, bounded retry schedule so the budget
	// exhausts quickly: schedule length 2 => 2 retries => 3 total attempts.
	cfg := configs.Basic(t, configs.BasicOpts{LogStorage: configs.LogStorageTypeClickHouse})
	cfg.RetrySchedule = []int{1, 1}
	cfg.RetryPollBackoffMs = 50
	cfg.LogBatchThresholdSeconds = 0 // immediate flush so /attempts is reliable
	require.NoError(t, cfg.Validate(config.Flags{}))
	configs.ApplyMigrations(t, &cfg)

	appDone := make(chan struct{})
	go func() {
		defer close(appDone)
		application := app.New(&cfg)
		if err := application.Run(ctx); err != nil {
			log.Println("Application stopped:", err)
		}
	}()
	defer func() {
		cancel()
		<-appDone
	}()

	waitForHealthy(t, cfg.APIPort, 5*time.Second)

	client := newRegressionHTTPClient(cfg.APIKey)
	apiURL := fmt.Sprintf("http://localhost:%d/api/v1", cfg.APIPort)

	tenantID := fmt.Sprintf("tenant_format_%d", time.Now().UnixNano())
	destinationID := fmt.Sprintf("dest_format_%d", time.Now().UnixNano())
	eventID := fmt.Sprintf("evt_format_%d", time.Now().UnixNano())

	// Create tenant.
	status := client.doJSON(t, http.MethodPut, apiURL+"/tenants/"+tenantID, nil, nil)
	require.Equal(t, 201, status, "failed to create tenant")

	// Create an aws_s3 destination whose key_template is valid at creation but cannot be
	// evaluated for an event lacking metadata.operationId (join over a nil value fails).
	status = client.doJSON(t, http.MethodPost, apiURL+"/tenants/"+tenantID+"/destinations", map[string]any{
		"id":     destinationID,
		"type":   "aws_s3",
		"topics": "*",
		"config": map[string]any{
			"bucket":        bucket,
			"region":        "us-east-1",
			"endpoint":      endpoint,
			"storage_class": "STANDARD",
			"key_template":  `join('/', ['prefix', metadata.operationId])`,
		},
		"credentials": map[string]any{
			"key":    "test",
			"secret": "test",
		},
	}, nil)
	require.Equal(t, 201, status, "failed to create aws_s3 destination")

	// Publish a retry-eligible event WITHOUT metadata.operationId -> formatting fails at delivery.
	status = client.doJSON(t, http.MethodPost, apiURL+"/publish", map[string]any{
		"id":                 eventID,
		"tenant_id":          tenantID,
		"topic":              "user.created",
		"eligible_for_retry": true,
		"metadata":           map[string]any{"foo": "bar"},
		"data":               map[string]any{"hello": "world"},
	}, nil)
	require.Equal(t, 202, status, "failed to publish event")

	attemptsURL := apiURL + "/attempts?tenant_id=" + tenantID + "&event_id=" + eventID + "&dir=asc&include=response_data"
	pollAttempts := func(t *testing.T, minCount int, timeout time.Duration) []map[string]any {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			var resp struct {
				Models []map[string]any `json:"models"`
			}
			s := client.doJSON(t, http.MethodGet, attemptsURL, nil, &resp)
			if s == http.StatusOK && len(resp.Models) >= minCount {
				return resp.Models
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %d attempts", minCount)
		return nil
	}

	// (2) + (3): retries run on the normal schedule and produce recorded failed attempts.
	// 1 initial + 2 scheduled retries = 3 attempts. If the fix regressed, the format error
	// would nack/dead-letter the message and ZERO attempts would be logged -> this times out.
	attempts := pollAttempts(t, 3, 15*time.Second)
	for i, atm := range attempts {
		require.Equal(t, "failed", atm["status"], "attempt %d should be a failed delivery", i+1)
		// (2) the attempt carries the format error as a normal, customer-facing delivery error.
		if rd, ok := atm["response_data"].(map[string]any); ok {
			require.Equal(t, "could not format event for delivery", rd["error"],
				"attempt %d should record the format error", i+1)
		} else {
			t.Fatalf("attempt %d missing response_data", i+1)
		}
	}

	// Budget exhausted: after the schedule completes, no further attempts appear. A
	// dead-letter/requeue loop would keep producing attempts (or none at all) instead
	// of stopping cleanly at the retry budget.
	time.Sleep(2 * time.Second)
	var finalResp struct {
		Models []map[string]any `json:"models"`
	}
	client.doJSON(t, http.MethodGet, attemptsURL, nil, &finalResp)
	require.Len(t, finalResp.Models, 3,
		"should have exactly 3 attempts (1 initial + 2 retries) then stop — not requeue into a DLQ")

	// (1) nothing was ever written to S3 — the failure happened before any PutObject.
	listed, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
	require.NoError(t, err)
	require.Empty(t, listed.Contents, "no object should have been written to S3")
}

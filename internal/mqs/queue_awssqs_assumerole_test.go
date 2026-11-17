package mqs_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAWS answers STS AssumeRole and SQS GetQueueUrl, recording what it saw.
type fakeAWS struct {
	mu          sync.Mutex
	assumeRole  url.Values
	stsAuth     string
	sqsAuth     string
	sqsSecurity string
}

func (f *fakeAWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasPrefix(r.Header.Get("X-Amz-Target"), "AmazonSQS.") {
		f.sqsAuth = r.Header.Get("Authorization")
		f.sqsSecurity = r.Header.Get("X-Amz-Security-Token")
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_, _ = w.Write([]byte(`{"QueueUrl":"http://sqs.local/123456789012/publish"}`))
		return
	}
	f.assumeRole, _ = url.ParseQuery(string(body))
	f.stsAuth = r.Header.Get("Authorization")
	w.Header().Set("Content-Type", "text/xml")
	_, _ = w.Write([]byte(`<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><AssumeRoleResult><Credentials><AccessKeyId>ASIAASSUMED</AccessKeyId><SecretAccessKey>assumed-secret</SecretAccessKey><SessionToken>assumed-token</SessionToken><Expiration>2999-01-01T00:00:00Z</Expiration></Credentials></AssumeRoleResult></AssumeRoleResponse>`))
}

// newFakeAWS starts a fake STS and SQS and points the SDK's STS client at it,
// with the machine's own AWS profile and config files out of the way.
func newFakeAWS(t *testing.T) (*fakeAWS, string) {
	t.Helper()
	fake := &fakeAWS{}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	t.Setenv("AWS_ENDPOINT_URL_STS", server.URL)
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "false")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "credentials"))
	return fake, server.URL
}

func TestAWSQueue_InitSDK_AssumeRole(t *testing.T) {
	fake, endpoint := newFakeAWS(t)

	queue := mqs.NewAWSQueue(&mqs.AWSSQSConfig{
		Endpoint:                  endpoint,
		Region:                    "us-east-1",
		ServiceAccountCredentials: "AKIABASE:base-secret:",
		Topic:                     "publish",
		RoleARN:                   "arn:aws:iam::123456789012:role/queue-reader",
		ExternalID:                "ext-123",
		RoleSessionName:           "reader",
	})
	require.NoError(t, queue.InitSDK(t.Context()))

	fake.mu.Lock()
	defer fake.mu.Unlock()
	assert.Equal(t, "AssumeRole", fake.assumeRole.Get("Action"))
	assert.Equal(t, "arn:aws:iam::123456789012:role/queue-reader", fake.assumeRole.Get("RoleArn"))
	assert.Equal(t, "ext-123", fake.assumeRole.Get("ExternalId"))
	assert.Equal(t, "reader", fake.assumeRole.Get("RoleSessionName"))
	assert.Contains(t, fake.stsAuth, "Credential=AKIABASE/", "the role is assumed with the base credentials")
	assert.Contains(t, fake.sqsAuth, "Credential=ASIAASSUMED/", "the queue is accessed as the assumed role")
	assert.Equal(t, "assumed-token", fake.sqsSecurity)
}

func TestAWSQueue_InitSDK_NoRole(t *testing.T) {
	fake, endpoint := newFakeAWS(t)

	queue := mqs.NewAWSQueue(&mqs.AWSSQSConfig{
		Endpoint:                  endpoint,
		Region:                    "us-east-1",
		ServiceAccountCredentials: "AKIABASE:base-secret:",
		Topic:                     "publish",
	})
	require.NoError(t, queue.InitSDK(t.Context()))

	fake.mu.Lock()
	defer fake.mu.Unlock()
	assert.Empty(t, fake.assumeRole, "no role configured: STS is not called")
	assert.Contains(t, fake.sqsAuth, "Credential=AKIABASE/")
}

func TestAWSQueue_InitSDK_AssumeRoleDefaultSessionName(t *testing.T) {
	fake, endpoint := newFakeAWS(t)

	queue := mqs.NewAWSQueue(&mqs.AWSSQSConfig{
		Endpoint:                  endpoint,
		Region:                    "us-east-1",
		ServiceAccountCredentials: "AKIABASE:base-secret:",
		Topic:                     "publish",
		RoleARN:                   "arn:aws:iam::123456789012:role/queue-reader",
	})
	require.NoError(t, queue.InitSDK(t.Context()))

	fake.mu.Lock()
	defer fake.mu.Unlock()
	assert.Equal(t, "outpost", fake.assumeRole.Get("RoleSessionName"))
	assert.Empty(t, fake.assumeRole.Get("ExternalId"))
}

package destawss3_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A key_template can compile and still panic when it is evaluated: go-jmespath's
// merge() type-asserts its arguments, so merge() over an array panics on the
// first event. The destination passes validation, and the delivery must come
// back as a failed attempt instead of a panic.
func TestAWSS3_KeyTemplatePanicIsFailedAttempt(t *testing.T) {
	t.Parallel()

	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer s3Server.Close()

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_s3"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"bucket":       "my-bucket",
			"region":       "us-east-1",
			"endpoint":     s3Server.URL,
			"key_template": `to_string(merge([{a: data}]))`,
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":    "test-key",
			"secret": "test-secret",
		}),
	)

	provider, err := destawss3.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)
	require.NoError(t, provider.Validate(context.Background(), &destination),
		"the template compiles, so the destination is accepted")

	registry := destregistry.NewRegistry(&destregistry.Config{}, testutil.CreateTestLogger(t))
	require.NoError(t, registry.RegisterProvider("aws_s3", provider))

	event := models.Event{
		ID:    "evt_1",
		Topic: "user.created",
		Time:  time.Now(),
		Data:  json.RawMessage(`{"user_id":"u_1"}`),
	}

	var attempt *models.Attempt
	var publishErr error
	require.NotPanics(t, func() {
		attempt, publishErr = registry.PublishEvent(context.Background(), &destination, &event)
	})

	require.NotNil(t, attempt, "a failed attempt must be recorded so the message is acked")
	assert.Equal(t, "failed", attempt.Status)
	assert.Equal(t, "ERR", attempt.Code)

	var pubErr *destregistry.ErrDestinationPublishAttempt
	require.ErrorAs(t, publishErr, &pubErr)
	assert.Equal(t, "format_failed", pubErr.Data["error"])
	assert.Equal(t, "key_template could not be evaluated: the expression failed on an unsupported value", attempt.ResponseData["error"])
}

func TestAWSS3Publisher_Publish_TemplateEvaluationErrorMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "panic in merge",
			template: `to_string(merge([{a: data}]))`,
			want:     "key_template could not be evaluated: the expression failed on an unsupported value",
		},
		{
			name:     "panic in contains",
			template: "contains(`[{}]`, `{}`)",
			want:     "key_template could not be evaluated: the expression failed on an unsupported value",
		},
		{
			name:     "ordinary evaluation error",
			template: `join('/', ['prefix', metadata.operationId])`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			publisher := destawss3.NewAWSS3Publisher(destregistry.NewBasePublisher(), nil, "my-bucket", tt.template, "STANDARD")
			event := models.Event{ID: "evt_1", Time: time.Now(), Data: json.RawMessage(`{}`)}

			var delivery *destregistry.Delivery
			var err error
			require.NotPanics(t, func() {
				delivery, err = publisher.Publish(context.Background(), &event)
			})
			require.NotNil(t, delivery)
			assert.Equal(t, "failed", delivery.Status)
			require.Error(t, err)
			if tt.want != "" {
				assert.Equal(t, tt.want, delivery.Response["error"])
			} else {
				assert.Contains(t, delivery.Response["error"], "key_template could not be evaluated: ")
			}
		})
	}
}

package destawss3_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A key_template can compile and still panic when evaluated: go-jmespath's
// merge() type-asserts its arguments, so merge() over an array panics on the
// first event. The destination passes validation; PublishEvent must return an
// error and no attempt, so the delivery handler nacks the message.
func TestAWSS3_KeyTemplatePanicReturnsError(t *testing.T) {
	t.Parallel()

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_s3"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"bucket":       "my-bucket",
			"region":       "us-east-1",
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

	assert.Nil(t, attempt, "no attempt is recorded for a panic")
	require.ErrorContains(t, publishErr, "panic publishing to aws_s3 destination")
}

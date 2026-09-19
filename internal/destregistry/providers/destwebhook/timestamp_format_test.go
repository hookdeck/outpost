package destwebhook_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookPublisher_TimestampFormat(t *testing.T) {
	t.Parallel()

	format := func(t *testing.T, provider *destwebhook.WebhookDestination, deliveryMetadata map[string]string) (time.Time, string) {
		t.Helper()
		opts := []func(*models.Destination){
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{"url": "http://example.com/webhook"}),
			testutil.DestinationFactory.WithCredentials(map[string]string{"secret": "test-secret"}),
		}
		if deliveryMetadata != nil {
			opts = append(opts, testutil.DestinationFactory.WithDeliveryMetadata(deliveryMetadata))
		}
		destination := testutil.DestinationFactory.Any(opts...)
		publisher, err := provider.CreatePublisher(context.Background(), &destination)
		require.NoError(t, err)
		event := testutil.EventFactory.Any(testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}))
		before := time.Now()
		req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
		require.NoError(t, err)
		return before, req.Header.Get("x-outpost-timestamp")
	}

	t.Run("rfc3339 by default", func(t *testing.T) {
		_, value := format(t, NewTestProvider(t), nil)
		_, err := time.Parse(time.RFC3339, value)
		assert.NoError(t, err, "timestamp %q", value)
	})

	t.Run("unix renders seconds", func(t *testing.T) {
		before, value := format(t, NewTestProvider(t, destwebhook.WithTimestampFormat(destwebhook.TimestampFormatUnix)), nil)
		seconds, err := strconv.ParseInt(value, 10, 64)
		require.NoError(t, err, "timestamp %q", value)
		assert.InDelta(t, before.Unix(), seconds, 2)
	})

	t.Run("unix leaves a metadata override alone", func(t *testing.T) {
		_, value := format(t, NewTestProvider(t, destwebhook.WithTimestampFormat(destwebhook.TimestampFormatUnix)),
			map[string]string{"timestamp": "custom"})
		assert.Equal(t, "custom", value)
	})

	t.Run("unknown format fails construction", func(t *testing.T) {
		_, err := newTestProvider(destwebhook.WithTimestampFormat("millis"))
		assert.ErrorContains(t, err, `invalid timestamp format "millis"`)
	})
}

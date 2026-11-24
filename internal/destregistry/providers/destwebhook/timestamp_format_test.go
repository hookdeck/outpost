package destwebhook_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
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

	t.Run("iso8601_milli_no_offset renders UTC milliseconds without an offset", func(t *testing.T) {
		before, value := format(t, NewTestProvider(t, destwebhook.WithTimestampFormat(destwebhook.TimestampFormatISO8601MilliNoOffset)), nil)
		assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}$`, value)
		parsed, err := time.Parse(destwebhook.TimestampLayoutISO8601MilliNoOffset, value)
		require.NoError(t, err, "timestamp %q", value)
		assert.WithinDuration(t, before, parsed, 2*time.Second)
	})

	t.Run("iso8601_milli_no_offset leaves a metadata override alone", func(t *testing.T) {
		_, value := format(t, NewTestProvider(t, destwebhook.WithTimestampFormat(destwebhook.TimestampFormatISO8601MilliNoOffset)),
			map[string]string{"timestamp": "custom"})
		assert.Equal(t, "custom", value)
	})

	t.Run("unknown format fails construction", func(t *testing.T) {
		_, err := newTestProvider(destwebhook.WithTimestampFormat("millis"))
		assert.ErrorContains(t, err, `invalid timestamp format "millis"`)
	})
}

// A receiver that signs the timestamp header verbatim (e.g. Orb's
// "v1:<timestamp>:<body>") verifies when the content template renders
// .Timestamp with the iso8601_milli_no_offset layout.
func TestWebhookPublisher_ISO8601MilliNoOffsetTimestampSigned(t *testing.T) {
	t.Parallel()

	provider := NewTestProvider(t,
		destwebhook.WithTimestampFormat(destwebhook.TimestampFormatISO8601MilliNoOffset),
		destwebhook.WithSignatureContentTemplate(`v1:{{.Timestamp.UTC.Format "2006-01-02T15:04:05.000"}}:{{.Body}}`),
		destwebhook.WithSignatureHeaderTemplate(`v1={{.Signatures | join " v1="}}`),
	)
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{"url": "http://example.com/webhook"}),
		testutil.DestinationFactory.WithCredentials(map[string]string{"secret": "test-secret"}),
	)
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	event := testutil.EventFactory.Any(testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}))
	req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
	require.NoError(t, err)

	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write([]byte("v1:" + req.Header.Get("x-outpost-timestamp") + ":" + string(body)))
	assert.Equal(t, "v1="+hex.EncodeToString(mac.Sum(nil)), req.Header.Get("x-outpost-signature"))
}

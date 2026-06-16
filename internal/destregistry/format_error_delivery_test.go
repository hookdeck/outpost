package destregistry_test

import (
	"errors"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFormatErrorDelivery(t *testing.T) {
	t.Parallel()

	rawErr := errors.New("failed to evaluate key template: invalid type")

	t.Run("returns a failed delivery and a publish-attempt error", func(t *testing.T) {
		t.Parallel()

		delivery, err := destregistry.NewFormatErrorDelivery("aws_s3", "", rawErr)

		// A non-nil failed delivery means the registry records an attempt and acks
		// the message instead of nacking it into the DLQ.
		require.NotNil(t, delivery)
		assert.Equal(t, "failed", delivery.Status)
		assert.Equal(t, "ERR", delivery.Code)

		var pubErr *destregistry.ErrDestinationPublishAttempt
		require.ErrorAs(t, err, &pubErr)
		assert.Equal(t, "aws_s3", pubErr.Provider)
		assert.Equal(t, "format_failed", pubErr.Data["error"])
		// Raw Go error is carried on the error for logs/telemetry...
		assert.Equal(t, rawErr, pubErr.Err)
	})

	t.Run("uses a generic message when none is given", func(t *testing.T) {
		t.Parallel()

		delivery, _ := destregistry.NewFormatErrorDelivery("aws_s3", "", rawErr)

		// ...but is NOT persisted on the attempt; the customer-facing response is generic.
		assert.Equal(t, "could not format event for delivery", delivery.Response["error"])
		assert.NotContains(t, delivery.Response["error"], "key template")
	})

	t.Run("uses the provided message when given", func(t *testing.T) {
		t.Parallel()

		delivery, _ := destregistry.NewFormatErrorDelivery("aws_s3", "could not build S3 object key", rawErr)
		assert.Equal(t, "could not build S3 object key", delivery.Response["error"])
	})
}

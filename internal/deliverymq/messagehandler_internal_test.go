package deliverymq

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/assert"
)

// Both ack/nack decision points share one permanent-error list.
func TestPermanentPreDeliveryErrors(t *testing.T) {
	t.Parallel()
	h := &messageHandler{}

	for _, err := range []error{
		tenantstore.ErrDestinationDeleted,
		errDestinationDisabled,
		errDestinationExpired,
		errDestinationGenerationMismatch,
		errRetryParked,
	} {
		assert.True(t, isPermanentPreDeliveryErr(err), "%v", err)
		assert.True(t, isPermanentPreDeliveryErr(fmt.Errorf("wrapped: %w", err)), "%v", err)
		assert.False(t, h.shouldNackError(&PreDeliveryError{err: err}), "%v acks", err)
	}

	for _, err := range []error{
		tenantstore.ErrDestinationNotFound,
		errDestinationDisabledAgain,
		errors.New("redis: connection refused"),
	} {
		assert.False(t, isPermanentPreDeliveryErr(err), "%v", err)
		assert.True(t, h.shouldNackError(&PreDeliveryError{err: err}), "%v nacks", err)
	}
}

// Destinations without an expiry never expire.
func TestDestinationExpired_NoExpiry(t *testing.T) {
	t.Parallel()
	assert.False(t, destinationExpired(&models.Destination{ID: "d"}, time.Now()))
}

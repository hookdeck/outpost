package deliverymq

import (
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

// WithExpiryCheckForTest overrides how the handler decides that a destination
// has expired, so expiry can be exercised without a store.
func WithExpiryCheckForTest(isExpired func(destination *models.Destination, now time.Time) bool) MessageHandlerOption {
	return func(h *messageHandler) {
		h.isExpired = isExpired
	}
}

// WithClockForTest overrides the handler's clock.
func WithClockForTest(now func() time.Time) MessageHandlerOption {
	return func(h *messageHandler) {
		h.now = now
	}
}

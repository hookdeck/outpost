package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

var (
	ErrAlreadyDelivered    = errors.New("event already successfully delivered to destination")
	ErrNoFailedDelivery    = errors.New("no failed delivery found for this event and destination")
	ErrDestinationDisabled = errors.New("destination is disabled")
)

type RetryHandlers struct {
	logger      *otelzap.Logger
	entityStore models.EntityStore
	logStore    models.LogStore
	deliveryMQ  *deliverymq.DeliveryMQ
}

func NewRetryHandlers(logger *otelzap.Logger, entityStore models.EntityStore, logStore models.LogStore, deliveryMQ *deliverymq.DeliveryMQ) *RetryHandlers {
	return &RetryHandlers{
		logger:      logger,
		entityStore: entityStore,
		logStore:    logStore,
		deliveryMQ:  deliveryMQ,
	}
}

// isEligibleForManualRetry checks if a destination/event pair is eligible for manual retry based on delivery history.
// Note: This function deliberately ignores event.EligibleForRetry since manual retries should be allowed
// regardless of the event's automatic retry settings.
func isEligibleForManualRetry(destination *models.Destination, deliveries []*models.Delivery) error {
	if destination.DisabledAt != nil {
		return ErrDestinationDisabled
	}

	var hasFailedDelivery bool
	for _, delivery := range deliveries {
		if delivery.DestinationID == destination.ID {
			if delivery.Status == models.DeliveryStatusOK {
				return ErrAlreadyDelivered
			}
			if delivery.Status == models.DeliveryStatusFailed {
				hasFailedDelivery = true
			}
		}
	}

	if !hasFailedDelivery {
		return ErrNoFailedDelivery
	}

	return nil
}

func (h *RetryHandlers) Retry(c *gin.Context) {
	tenantID := c.Param("tenantID")
	destinationID := c.Param("destinationID")
	eventID := c.Param("eventID")

	// 1. Retrieve destination & event data
	destination, err := h.entityStore.RetrieveDestination(c, tenantID, destinationID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if destination == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("destination"))
		return
	}

	event, err := h.logStore.RetrieveEvent(c, tenantID, eventID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if event == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("event"))
		return
	}

	// 2. Get delivery history
	deliveries, err := h.logStore.ListDelivery(c, models.ListDeliveryRequest{EventID: eventID})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	// 3. Validate retry eligibility
	if err := isEligibleForManualRetry(destination, deliveries); err != nil {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
		return
	}

	// 4. Initiate redelivery
	deliveryEvent := models.NewManualDeliveryEvent(*event, destination.ID)

	if err := h.deliveryMQ.Publish(c, deliveryEvent); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	c.Status(http.StatusAccepted)
}

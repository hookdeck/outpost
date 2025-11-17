package apirouter

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"go.uber.org/zap"
)

var (
	ErrDestinationDisabled = errors.New("destination is disabled")
)

type RetryHandlers struct {
	logger      *logging.Logger
	entityStore models.EntityStore
	logStore    logstore.LogStore
	deliveryMQ  *deliverymq.DeliveryMQ
}

func NewRetryHandlers(logger *logging.Logger, entityStore models.EntityStore, logStore logstore.LogStore, deliveryMQ *deliverymq.DeliveryMQ) *RetryHandlers {
	return &RetryHandlers{
		logger:      logger,
		entityStore: entityStore,
		logStore:    logStore,
		deliveryMQ:  deliveryMQ,
	}
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
	if destination.DisabledAt != nil {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(ErrDestinationDisabled))
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

	// 2. Initiate redelivery
	deliveryEvent := models.NewManualDeliveryEvent(*event, destination.ID)

	if err := h.deliveryMQ.Publish(c, deliveryEvent); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	h.logger.Ctx(c).Audit("manual retry initiated",
		zap.String("event_id", event.ID),
		zap.String("destination_id", destination.ID))

	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
	})
}

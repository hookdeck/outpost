package apirouter

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"go.uber.org/zap"
)

type RetryHandlers struct {
	logger      *logging.Logger
	entityStore models.EntityStore
	logStore    logstore.LogStore
	deliveryMQ  *deliverymq.DeliveryMQ
}

func NewRetryHandlers(
	logger *logging.Logger,
	entityStore models.EntityStore,
	logStore logstore.LogStore,
	deliveryMQ *deliverymq.DeliveryMQ,
) *RetryHandlers {
	return &RetryHandlers{
		logger:      logger,
		entityStore: entityStore,
		logStore:    logStore,
		deliveryMQ:  deliveryMQ,
	}
}

// RetryDelivery handles POST /:tenantID/deliveries/:deliveryID/retry
// Constraints:
// - Only the latest delivery for an event+destination pair can be retried
// - Destination must exist and be enabled
func (h *RetryHandlers) RetryDelivery(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	deliveryID := c.Param("deliveryID")

	// 1. Look up delivery by ID
	deliveryRecord, err := h.logStore.RetrieveDelivery(c.Request.Context(), logstore.RetrieveDeliveryRequest{
		TenantID:   tenant.ID,
		DeliveryID: deliveryID,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if deliveryRecord == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("delivery"))
		return
	}

	// 2. Check destination exists and is enabled
	destination, err := h.entityStore.RetrieveDestination(c.Request.Context(), tenant.ID, deliveryRecord.Delivery.DestinationID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if destination == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("destination"))
		return
	}
	if destination.DisabledAt != nil {
		AbortWithError(c, http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Destination is disabled",
			Data: map[string]string{
				"error": "destination_disabled",
			},
		})
		return
	}

	// 3. Create and publish manual delivery task
	task := models.NewManualDeliveryTask(*deliveryRecord.Event, deliveryRecord.Delivery.DestinationID)

	if err := h.deliveryMQ.Publish(c.Request.Context(), task); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	h.logger.Ctx(c.Request.Context()).Audit("manual retry initiated",
		zap.String("delivery_id", deliveryID),
		zap.String("event_id", deliveryRecord.Event.ID),
		zap.String("tenant_id", tenant.ID),
		zap.String("destination_id", deliveryRecord.Delivery.DestinationID),
		zap.String("destination_type", destination.Type))

	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
	})
}

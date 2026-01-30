package apirouter

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"go.uber.org/zap"
)

type RetryHandlers struct {
	logger      *logging.Logger
	tenantStore tenantstore.TenantStore
	logStore    logstore.LogStore
	deliveryMQ  *deliverymq.DeliveryMQ
}

func NewRetryHandlers(
	logger *logging.Logger,
	tenantStore tenantstore.TenantStore,
	logStore logstore.LogStore,
	deliveryMQ *deliverymq.DeliveryMQ,
) *RetryHandlers {
	return &RetryHandlers{
		logger:      logger,
		tenantStore: tenantStore,
		logStore:    logStore,
		deliveryMQ:  deliveryMQ,
	}
}

// RetryAttempt handles POST /:tenantID/attempts/:attemptID/retry
// Constraints:
// - Only the latest attempt for an event+destination pair can be retried
// - Destination must exist and be enabled
func (h *RetryHandlers) RetryAttempt(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	attemptID := c.Param("attemptID")

	// 1. Look up attempt by ID
	attemptRecord, err := h.logStore.RetrieveAttempt(c.Request.Context(), logstore.RetrieveAttemptRequest{
		TenantID:  tenant.ID,
		AttemptID: attemptID,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if attemptRecord == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("attempt"))
		return
	}

	// 2. Check destination exists and is enabled
	destination, err := h.tenantStore.RetrieveDestination(c.Request.Context(), tenant.ID, attemptRecord.Attempt.DestinationID)
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
	task := models.NewManualDeliveryTask(*attemptRecord.Event, attemptRecord.Attempt.DestinationID)

	if err := h.deliveryMQ.Publish(c.Request.Context(), task); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	h.logger.Ctx(c.Request.Context()).Audit("manual retry initiated",
		zap.String("attempt_id", attemptID),
		zap.String("event_id", attemptRecord.Event.ID),
		zap.String("tenant_id", tenant.ID),
		zap.String("destination_id", attemptRecord.Attempt.DestinationID),
		zap.String("destination_type", destination.Type))

	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
	})
}

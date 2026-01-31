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

type retryRequest struct {
	EventID       string `json:"event_id" binding:"required"`
	DestinationID string `json:"destination_id" binding:"required"`
}

// Retry handles POST /retry
// Accepts { event_id, destination_id } in body.
// Looks up the event, verifies the destination exists and is enabled, then publishes a manual delivery task.
func (h *RetryHandlers) Retry(c *gin.Context) {
	var req retryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
		return
	}

	tenantID := tenantIDFromContext(c)

	// 1. Look up event by ID
	event, err := h.logStore.RetrieveEvent(c.Request.Context(), logstore.RetrieveEventRequest{
		TenantID: tenantID,
		EventID:  req.EventID,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if event == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("event"))
		return
	}

	// Authz: JWT tenant can only retry their own events
	if tenant := tenantFromContext(c); tenant != nil {
		if event.TenantID != tenant.ID {
			AbortWithError(c, http.StatusNotFound, NewErrNotFound("event"))
			return
		}
	}

	// 2. Check destination exists and is enabled
	destination, err := h.tenantStore.RetrieveDestination(c.Request.Context(), event.TenantID, req.DestinationID)
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

	if !destination.MatchEvent(*event) {
		AbortWithError(c, http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "destination does not match event",
		})
		return
	}

	// 3. Create and publish manual delivery task
	task := models.NewManualDeliveryTask(*event, req.DestinationID)

	if err := h.deliveryMQ.Publish(c.Request.Context(), task); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	h.logger.Ctx(c.Request.Context()).Audit("manual retry initiated",
		zap.String("event_id", event.ID),
		zap.String("tenant_id", event.TenantID),
		zap.String("destination_id", req.DestinationID),
		zap.String("destination_type", destination.Type))

	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
	})
}

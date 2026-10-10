package apirouter

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"go.uber.org/zap"
)

type deliveryPublisher interface {
	Publish(ctx context.Context, task models.DeliveryTask) error
}

type RetryHandlers struct {
	logger               *logging.Logger
	tenantStore          tenantstore.TenantStore
	logStore             logstore.LogStore
	deliveryPublisher    deliveryPublisher
	topicsAllowWildcards bool
}

func NewRetryHandlers(
	logger *logging.Logger,
	tenantStore tenantstore.TenantStore,
	logStore logstore.LogStore,
	deliveryPublisher deliveryPublisher,
	topicsAllowWildcards bool,
) *RetryHandlers {
	return &RetryHandlers{
		logger:               logger,
		tenantStore:          tenantStore,
		logStore:             logStore,
		deliveryPublisher:    deliveryPublisher,
		topicsAllowWildcards: topicsAllowWildcards,
	}
}

type retryRequest struct {
	EventID       string `json:"event_id" binding:"required"`
	DestinationID string `json:"destination_id" binding:"required"`
}

// Retry handles POST /retry
// Accepts { event_id, destination_id } in body.
// Looks up the event, verifies the destination exists, is enabled, matches the event and has
// already been sent it, then publishes a manual delivery task.
func (h *RetryHandlers) Retry(c *gin.Context) {
	var req retryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		AbortWithValidationError(c, err)
		return
	}

	tenantID := tenantIDFromContext(c)

	// 1. Look up prior attempt (includes event data)
	listReq := logstore.ListAttemptRequest{
		EventIDs:       []string{req.EventID},
		DestinationIDs: []string{req.DestinationID},
		Limit:          1,
		SortOrder:      "desc",
	}
	if tenantID != "" {
		listReq.TenantIDs = []string{tenantID}
	}
	attemptResp, err := h.logStore.ListAttempt(c.Request.Context(), listReq)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	var event *models.Event
	var attemptNumber int
	hasAttempt := len(attemptResp.Data) > 0
	if hasAttempt {
		record := attemptResp.Data[0]
		event = record.Event
		attemptNumber = record.Attempt.AttemptNumber + 1
	} else {
		// No attempt for the pair: look the event up on its own to tell which part is missing.
		event, err = h.logStore.RetrieveEvent(c.Request.Context(), logstore.RetrieveEventRequest{
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
		if errors.Is(err, tenantstore.ErrDestinationDeleted) {
			AbortWithError(c, http.StatusNotFound, NewErrNotFound("destination"))
			return
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if destination == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("destination"))
		return
	}
	// Expired but not yet deleted (MCP subscriptions): no attempt is made
	// after expires_at, retries included.
	if destination.IsExpired(time.Now()) {
		AbortWithError(c, http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "Destination has expired",
			Data: map[string]string{
				"error": "destination_expired",
			},
		})
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

	if !destination.MatchEvent(*event, h.topicsAllowWildcards) {
		AbortWithError(c, http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "destination does not match event",
		})
		return
	}

	if !hasAttempt {
		AbortWithError(c, http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "event has no attempt for this destination",
		})
		return
	}

	// 3. Create and publish manual delivery task
	task := models.NewManualDeliveryTask(*event, req.DestinationID, attemptNumber)

	if err := h.deliveryPublisher.Publish(c.Request.Context(), task); err != nil {
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

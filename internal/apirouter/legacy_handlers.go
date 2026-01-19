package apirouter

import (
	"errors"
	"net/http"
	"strconv"

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

// LegacyHandlers provides backward-compatible endpoints for the old API.
// These handlers are deprecated and will be removed in a future version.
type LegacyHandlers struct {
	logger      *logging.Logger
	entityStore models.EntityStore
	logStore    logstore.LogStore
	deliveryMQ  *deliverymq.DeliveryMQ
}

func NewLegacyHandlers(
	logger *logging.Logger,
	entityStore models.EntityStore,
	logStore logstore.LogStore,
	deliveryMQ *deliverymq.DeliveryMQ,
) *LegacyHandlers {
	return &LegacyHandlers{
		logger:      logger,
		entityStore: entityStore,
		logStore:    logStore,
		deliveryMQ:  deliveryMQ,
	}
}

// setDeprecationHeader adds deprecation warning headers to the response.
func setDeprecationHeader(c *gin.Context, newEndpoint string) {
	c.Header("Deprecation", "true")
	c.Header("X-Deprecated-Message", "This endpoint is deprecated. Use "+newEndpoint+" instead.")
}

// RetryByEventDestination handles the legacy retry endpoint:
// POST /:tenantID/destinations/:destinationID/events/:eventID/retry
//
// This shim finds the latest delivery for the event+destination pair and retries it.
// Deprecated: Use POST /:tenantID/deliveries/:deliveryID/retry instead.
func (h *LegacyHandlers) RetryByEventDestination(c *gin.Context) {
	setDeprecationHeader(c, "POST /:tenantID/deliveries/:deliveryID/retry")

	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	destinationID := c.Param("destinationID")
	eventID := c.Param("eventID")

	// 1. Check destination exists and is enabled
	destination, err := h.entityStore.RetrieveDestination(c.Request.Context(), tenant.ID, destinationID)
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

	// 2. Retrieve event
	event, err := h.logStore.RetrieveEvent(c.Request.Context(), logstore.RetrieveEventRequest{
		TenantID: tenant.ID,
		EventID:  eventID,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if event == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("event"))
		return
	}

	// 3. Create and publish retry delivery event
	deliveryEvent := models.NewManualDeliveryEvent(*event, destination.ID)

	if err := h.deliveryMQ.Publish(c.Request.Context(), deliveryEvent); err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	h.logger.Ctx(c.Request.Context()).Audit("manual retry initiated (legacy)",
		zap.String("event_id", event.ID),
		zap.String("destination_id", destination.ID))

	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
	})
}

// ListEventsByDestination handles the legacy endpoint:
// GET /:tenantID/destinations/:destinationID/events
//
// This shim queries deliveries filtered by destination and returns unique events.
// Deprecated: Use GET /:tenantID/deliveries?destination_id=X&include=event instead.
func (h *LegacyHandlers) ListEventsByDestination(c *gin.Context) {
	setDeprecationHeader(c, "GET /:tenantID/deliveries?destination_id=X&include=event")

	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	destinationID := c.Param("destinationID")

	// Parse and validate cursors (next/prev are mutually exclusive)
	cursors, errResp := ParseCursors(c)
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}

	// Parse pagination params
	limit := 100
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	// Query deliveries for this destination with pagination
	response, err := h.logStore.ListDeliveryEvent(c.Request.Context(), logstore.ListDeliveryEventRequest{
		TenantID:       tenant.ID,
		DestinationIDs: []string{destinationID},
		Limit:          limit,
		Next:           cursors.Next,
		Prev:           cursors.Prev,
		SortOrder:      "desc",
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	// Extract unique events (by event ID, keep first occurrence)
	seen := make(map[string]bool)
	events := []models.Event{}
	for _, de := range response.Data {
		if !seen[de.Event.ID] {
			seen[de.Event.ID] = true
			events = append(events, de.Event)
		}
	}

	// Return empty array (not null) if no events
	if len(events) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"data":  []models.Event{},
			"next":  "",
			"prev":  "",
			"count": 0,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  events,
		"next":  response.Next,
		"prev":  response.Prev,
		"count": len(events),
	})
}

// RetrieveEventByDestination handles the legacy endpoint:
// GET /:tenantID/destinations/:destinationID/events/:eventID
//
// Deprecated: Use GET /:tenantID/events/:eventID instead.
func (h *LegacyHandlers) RetrieveEventByDestination(c *gin.Context) {
	setDeprecationHeader(c, "GET /:tenantID/events/:eventID")

	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	eventID := c.Param("eventID")
	// destinationID is available but not strictly needed for retrieval

	event, err := h.logStore.RetrieveEvent(c.Request.Context(), logstore.RetrieveEventRequest{
		TenantID: tenant.ID,
		EventID:  eventID,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if event == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("event"))
		return
	}

	c.JSON(http.StatusOK, event)
}

// LegacyDeliveryResponse matches the old delivery response format.
type LegacyDeliveryResponse struct {
	ID           string                 `json:"id"`
	DeliveredAt  string                 `json:"delivered_at"`
	Status       string                 `json:"status"`
	Code         string                 `json:"code"`
	ResponseData map[string]interface{} `json:"response_data"`
}

// ListDeliveriesByEvent handles the legacy endpoint:
// GET /:tenantID/events/:eventID/deliveries
//
// Deprecated: Use GET /:tenantID/deliveries?event_id=X instead.
func (h *LegacyHandlers) ListDeliveriesByEvent(c *gin.Context) {
	setDeprecationHeader(c, "GET /:tenantID/deliveries?event_id=X")

	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	eventID := c.Param("eventID")

	// Query deliveries for this event
	response, err := h.logStore.ListDeliveryEvent(c.Request.Context(), logstore.ListDeliveryEventRequest{
		TenantID:  tenant.ID,
		EventID:   eventID,
		Limit:     100,
		SortOrder: "desc",
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	// Return empty array (not null) if no deliveries
	if len(response.Data) == 0 {
		c.JSON(http.StatusOK, []LegacyDeliveryResponse{})
		return
	}

	// Transform to legacy delivery response format (bare array)
	deliveries := make([]LegacyDeliveryResponse, len(response.Data))
	for i, de := range response.Data {
		deliveries[i] = LegacyDeliveryResponse{
			ID:           de.Delivery.ID,
			DeliveredAt:  de.Delivery.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Status:       de.Delivery.Status,
			Code:         de.Delivery.Code,
			ResponseData: de.Delivery.ResponseData,
		}
	}

	c.JSON(http.StatusOK, deliveries)
}

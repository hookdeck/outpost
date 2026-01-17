package apirouter

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
)

type LogHandlers struct {
	logger   *logging.Logger
	logStore logstore.LogStore
}

func NewLogHandlers(
	logger *logging.Logger,
	logStore logstore.LogStore,
) *LogHandlers {
	return &LogHandlers{
		logger:   logger,
		logStore: logStore,
	}
}

// parseQueryArray parses a query parameter that can be specified as repeated params
// (e.g., ?topic=a&topic=b) or comma-separated (e.g., ?topic=a,b) or both.
func parseQueryArray(c *gin.Context, key string) []string {
	var result []string
	for _, v := range c.QueryArray(key) {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}

// parseLimit parses the limit query parameter with a default and maximum value.
// If the provided limit exceeds maxLimit, it is capped at maxLimit.
func parseLimit(c *gin.Context, defaultLimit, maxLimit int) int {
	limit := defaultLimit
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return limit
}

// IncludeOptions represents which fields to include in the response
type IncludeOptions struct {
	Event        bool
	EventData    bool
	Destination  bool
	ResponseData bool
}

func parseIncludeOptions(c *gin.Context) IncludeOptions {
	opts := IncludeOptions{}
	for _, e := range parseQueryArray(c, "include") {
		switch e {
		case "event":
			opts.Event = true
		case "event.data":
			opts.Event = true
			opts.EventData = true
		case "destination":
			opts.Destination = true
		case "response_data":
			opts.ResponseData = true
		}
	}
	return opts
}

// API Response types

// APIDelivery is the API response for a delivery
type APIDelivery struct {
	ID           string                 `json:"id"`
	Status       string                 `json:"status"`
	DeliveredAt  time.Time              `json:"delivered_at"`
	Code         string                 `json:"code,omitempty"`
	ResponseData map[string]interface{} `json:"response_data,omitempty"`
	Attempt      int                    `json:"attempt"`

	// Expandable fields - string (ID) or object depending on expand
	Event       interface{} `json:"event"`
	Destination string      `json:"destination"`
}

// APIEventSummary is the event object when expand=event (without data)
type APIEventSummary struct {
	ID               string            `json:"id"`
	Topic            string            `json:"topic"`
	Time             time.Time         `json:"time"`
	EligibleForRetry bool              `json:"eligible_for_retry"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// APIEventFull is the event object when expand=event.data
type APIEventFull struct {
	ID               string                 `json:"id"`
	Topic            string                 `json:"topic"`
	Time             time.Time              `json:"time"`
	EligibleForRetry bool                   `json:"eligible_for_retry"`
	Metadata         map[string]string      `json:"metadata,omitempty"`
	Data             map[string]interface{} `json:"data,omitempty"`
}

// APIEvent is the API response for retrieving a single event
type APIEvent struct {
	ID               string                 `json:"id"`
	Topic            string                 `json:"topic"`
	Time             time.Time              `json:"time"`
	EligibleForRetry bool                   `json:"eligible_for_retry"`
	Metadata         map[string]string      `json:"metadata,omitempty"`
	Data             map[string]interface{} `json:"data,omitempty"`
}

// ListDeliveriesResponse is the response for ListDeliveries
type ListDeliveriesResponse struct {
	Data []APIDelivery `json:"data"`
	Next string        `json:"next,omitempty"`
	Prev string        `json:"prev,omitempty"`
}

// ListEventsResponse is the response for ListEvents
type ListEventsResponse struct {
	Data []APIEvent `json:"data"`
	Next string     `json:"next,omitempty"`
	Prev string     `json:"prev,omitempty"`
}

// toAPIDelivery converts a DeliveryEvent to APIDelivery with expand options
func toAPIDelivery(de *models.DeliveryEvent, opts IncludeOptions) APIDelivery {
	api := APIDelivery{
		Attempt:     de.Attempt,
		Destination: de.DestinationID,
	}

	// Set delivery fields if delivery exists
	if de.Delivery != nil {
		api.ID = de.Delivery.ID
		api.Status = de.Delivery.Status
		api.DeliveredAt = de.Delivery.Time
		api.Code = de.Delivery.Code
		if opts.ResponseData {
			api.ResponseData = de.Delivery.ResponseData
		}
	}

	// Handle event expansion
	if opts.EventData {
		api.Event = APIEventFull{
			ID:               de.Event.ID,
			Topic:            de.Event.Topic,
			Time:             de.Event.Time,
			EligibleForRetry: de.Event.EligibleForRetry,
			Metadata:         de.Event.Metadata,
			Data:             de.Event.Data,
		}
	} else if opts.Event {
		api.Event = APIEventSummary{
			ID:               de.Event.ID,
			Topic:            de.Event.Topic,
			Time:             de.Event.Time,
			EligibleForRetry: de.Event.EligibleForRetry,
			Metadata:         de.Event.Metadata,
		}
	} else {
		api.Event = de.Event.ID
	}

	// TODO: Handle destination expansion
	// This would require injecting EntityStore into LogHandlers and batch-fetching
	// destinations by ID. Consider if this is needed - clients can fetch destination
	// details separately via GET /destinations/:id if needed.

	return api
}

// ListDeliveries handles GET /:tenantID/deliveries
// Query params: event_id, destination_id, status, topic[], start, end, limit, next, prev, expand[], sort_order
func (h *LogHandlers) ListDeliveries(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	h.listDeliveriesInternal(c, tenant.ID)
}

// listDeliveriesInternal is the shared implementation for ListDeliveries and AdminListDeliveries.
// tenantID can be empty to query across all tenants (admin use case).
func (h *LogHandlers) listDeliveriesInternal(c *gin.Context, tenantID string) {
	// Parse time filters
	var start, end *time.Time
	if startStr := c.Query("start"); startStr != "" {
		t, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			AbortWithError(c, http.StatusUnprocessableEntity, ErrorResponse{
				Code:    http.StatusUnprocessableEntity,
				Message: "validation error",
				Data: map[string]string{
					"query.start": "invalid format, expected RFC3339",
				},
			})
			return
		}
		start = &t
	}
	if endStr := c.Query("end"); endStr != "" {
		t, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			AbortWithError(c, http.StatusUnprocessableEntity, ErrorResponse{
				Code:    http.StatusUnprocessableEntity,
				Message: "validation error",
				Data: map[string]string{
					"query.end": "invalid format, expected RFC3339",
				},
			})
			return
		}
		end = &t
	}

	// Parse limit (default 100, max 1000)
	limit := parseLimit(c, 100, 1000)

	// Parse destination_id (single value for now)
	var destinationIDs []string
	if destID := c.Query("destination_id"); destID != "" {
		destinationIDs = []string{destID}
	}

	// Parse sort_order (default: desc)
	sortOrder := c.Query("sort_order")
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		AbortWithError(c, http.StatusUnprocessableEntity, ErrorResponse{
			Code:    http.StatusUnprocessableEntity,
			Message: "validation error",
			Data: map[string]string{
				"query.sort_order": "must be 'asc' or 'desc'",
			},
		})
		return
	}

	// Build request
	req := logstore.ListDeliveryEventRequest{
		TenantID:       tenantID,
		EventID:        c.Query("event_id"),
		DestinationIDs: destinationIDs,
		Status:         c.Query("status"),
		Topics:         parseQueryArray(c, "topic"),
		Start:          start,
		End:            end,
		Limit:          limit,
		Next:           c.Query("next"),
		Prev:           c.Query("prev"),
		SortOrder:      sortOrder,
	}

	// Call logstore
	response, err := h.logStore.ListDeliveryEvent(c.Request.Context(), req)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	// Parse include options
	includeOpts := parseIncludeOptions(c)

	// Transform to API response
	apiDeliveries := make([]APIDelivery, len(response.Data))
	for i, de := range response.Data {
		apiDeliveries[i] = toAPIDelivery(de, includeOpts)
	}

	c.JSON(http.StatusOK, ListDeliveriesResponse{
		Data: apiDeliveries,
		Next: response.Next,
		Prev: response.Prev,
	})
}

// RetrieveEvent handles GET /:tenantID/events/:eventID
func (h *LogHandlers) RetrieveEvent(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	eventID := c.Param("eventID")
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
	c.JSON(http.StatusOK, APIEvent{
		ID:               event.ID,
		Topic:            event.Topic,
		Time:             event.Time,
		EligibleForRetry: event.EligibleForRetry,
		Metadata:         event.Metadata,
		Data:             event.Data,
	})
}

// RetrieveDelivery handles GET /:tenantID/deliveries/:deliveryID
func (h *LogHandlers) RetrieveDelivery(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	deliveryID := c.Param("deliveryID")

	deliveryEvent, err := h.logStore.RetrieveDeliveryEvent(c.Request.Context(), logstore.RetrieveDeliveryEventRequest{
		TenantID:   tenant.ID,
		DeliveryID: deliveryID,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if deliveryEvent == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("delivery"))
		return
	}

	// Parse include options
	includeOpts := parseIncludeOptions(c)

	c.JSON(http.StatusOK, toAPIDelivery(deliveryEvent, includeOpts))
}

// AdminListEvents handles GET /events (admin-only, cross-tenant)
// Query params: tenant_id (optional), destination_id, topic[], start, end, limit, next, prev, sort_order
func (h *LogHandlers) AdminListEvents(c *gin.Context) {
	h.listEventsInternal(c, c.Query("tenant_id"))
}

// AdminListDeliveries handles GET /deliveries (admin-only, cross-tenant)
// Query params: tenant_id (optional), event_id, destination_id, status, topic[], start, end, limit, next, prev, expand[], sort_order
func (h *LogHandlers) AdminListDeliveries(c *gin.Context) {
	h.listDeliveriesInternal(c, c.Query("tenant_id"))
}

// ListEvents handles GET /:tenantID/events
// Query params: destination_id, topic[], start, end, limit, next, prev, sort_order
func (h *LogHandlers) ListEvents(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	h.listEventsInternal(c, tenant.ID)
}

// listEventsInternal is the shared implementation for ListEvents and AdminListEvents.
// tenantID can be empty to query across all tenants (admin use case).
func (h *LogHandlers) listEventsInternal(c *gin.Context, tenantID string) {
	// Parse time filters
	var start, end *time.Time
	if startStr := c.Query("start"); startStr != "" {
		t, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			AbortWithError(c, http.StatusUnprocessableEntity, ErrorResponse{
				Code:    http.StatusUnprocessableEntity,
				Message: "validation error",
				Data: map[string]string{
					"query.start": "invalid format, expected RFC3339",
				},
			})
			return
		}
		start = &t
	}
	if endStr := c.Query("end"); endStr != "" {
		t, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			AbortWithError(c, http.StatusUnprocessableEntity, ErrorResponse{
				Code:    http.StatusUnprocessableEntity,
				Message: "validation error",
				Data: map[string]string{
					"query.end": "invalid format, expected RFC3339",
				},
			})
			return
		}
		end = &t
	}

	// Parse limit (default 100, max 1000)
	limit := parseLimit(c, 100, 1000)

	// Parse destination_id (single value for now)
	var destinationIDs []string
	if destID := c.Query("destination_id"); destID != "" {
		destinationIDs = []string{destID}
	}

	// Parse sort_order (default: desc)
	sortOrder := c.Query("sort_order")
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		AbortWithError(c, http.StatusUnprocessableEntity, ErrorResponse{
			Code:    http.StatusUnprocessableEntity,
			Message: "validation error",
			Data: map[string]string{
				"query.sort_order": "must be 'asc' or 'desc'",
			},
		})
		return
	}

	// Build request
	req := logstore.ListEventRequest{
		TenantID:       tenantID,
		DestinationIDs: destinationIDs,
		Topics:         parseQueryArray(c, "topic"),
		EventStart:     start,
		EventEnd:       end,
		Limit:          limit,
		Next:           c.Query("next"),
		Prev:           c.Query("prev"),
		SortOrder:      sortOrder,
	}

	// Call logstore
	response, err := h.logStore.ListEvent(c.Request.Context(), req)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	// Transform to API response
	apiEvents := make([]APIEvent, len(response.Data))
	for i, e := range response.Data {
		apiEvents[i] = APIEvent{
			ID:               e.ID,
			Topic:            e.Topic,
			Time:             e.Time,
			EligibleForRetry: e.EligibleForRetry,
			Metadata:         e.Metadata,
			Data:             e.Data,
		}
	}

	c.JSON(http.StatusOK, ListEventsResponse{
		Data: apiEvents,
		Next: response.Next,
		Prev: response.Prev,
	})
}

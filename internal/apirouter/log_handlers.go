package apirouter

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/cursor"
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
	Manual       bool                   `json:"manual"`

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

// DeliveryPaginatedResult is the paginated response for listing deliveries.
type DeliveryPaginatedResult struct {
	Models     []APIDelivery  `json:"models"`
	Pagination SeekPagination `json:"pagination"`
}

// EventPaginatedResult is the paginated response for listing events.
type EventPaginatedResult struct {
	Models     []APIEvent     `json:"models"`
	Pagination SeekPagination `json:"pagination"`
}

// toAPIDelivery converts a DeliveryEvent to APIDelivery with expand options
func toAPIDelivery(de *models.DeliveryEvent, opts IncludeOptions) APIDelivery {
	api := APIDelivery{
		Attempt:     de.Attempt,
		Manual:      de.Manual,
		Destination: de.DestinationID,
	}

	if de.Delivery != nil {
		api.ID = de.Delivery.ID
		api.Status = de.Delivery.Status
		api.DeliveredAt = de.Delivery.Time
		api.Code = de.Delivery.Code
		if opts.ResponseData {
			api.ResponseData = de.Delivery.ResponseData
		}
	}

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

func (h *LogHandlers) listDeliveriesInternal(c *gin.Context, tenantID string) {
	// Parse and validate dir (sort direction)
	dir, errResp := ParseDir(c)
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}
	if dir == "" {
		dir = "desc"
	}

	// Parse and validate order_by (time only)
	orderBy, errResp := ParseOrderBy(c, []string{"time"})
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}
	if orderBy == "" {
		orderBy = "time"
	}
	// Note: order_by is informational only for now - store always sorts by time
	_ = orderBy

	// Parse time date filters
	deliveryTimeFilter, errResp := ParseDateFilter(c, "time")
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}

	limit := parseLimit(c, 100, 1000)

	var destinationIDs []string
	if destID := c.Query("destination_id"); destID != "" {
		destinationIDs = []string{destID}
	}

	req := logstore.ListDeliveryEventRequest{
		TenantID:       tenantID,
		EventID:        c.Query("event_id"),
		DestinationIDs: destinationIDs,
		Status:         c.Query("status"),
		Topics:         parseQueryArray(c, "topic"),
		TimeFilter: logstore.TimeFilter{
			GTE: deliveryTimeFilter.GTE,
			LTE: deliveryTimeFilter.LTE,
			GT:  deliveryTimeFilter.GT,
			LT:  deliveryTimeFilter.LT,
		},
		Limit:     limit,
		Next:      c.Query("next"),
		Prev:      c.Query("prev"),
		SortOrder: dir,
	}

	response, err := h.logStore.ListDeliveryEvent(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, cursor.ErrInvalidCursor) || errors.Is(err, cursor.ErrVersionMismatch) {
			AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
			return
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	includeOpts := parseIncludeOptions(c)

	apiDeliveries := make([]APIDelivery, len(response.Data))
	for i, de := range response.Data {
		apiDeliveries[i] = toAPIDelivery(de, includeOpts)
	}

	c.JSON(http.StatusOK, DeliveryPaginatedResult{
		Models: apiDeliveries,
		Pagination: SeekPagination{
			OrderBy: orderBy,
			Dir:     dir,
			Limit:   limit,
			Next:    CursorToPtr(response.Next),
			Prev:    CursorToPtr(response.Prev),
		},
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

func (h *LogHandlers) listEventsInternal(c *gin.Context, tenantID string) {
	// Parse and validate dir (sort direction)
	dir, errResp := ParseDir(c)
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}
	if dir == "" {
		dir = "desc"
	}

	// Parse and validate order_by (time only)
	orderBy, errResp := ParseOrderBy(c, []string{"time"})
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}
	if orderBy == "" {
		orderBy = "time"
	}
	// Note: order_by is informational only for now - store always sorts by time
	_ = orderBy

	// Parse time date filters
	eventTimeFilter, errResp := ParseDateFilter(c, "time")
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}

	limit := parseLimit(c, 100, 1000)

	var destinationIDs []string
	if destID := c.Query("destination_id"); destID != "" {
		destinationIDs = []string{destID}
	}

	req := logstore.ListEventRequest{
		TenantID:       tenantID,
		DestinationIDs: destinationIDs,
		Topics:         parseQueryArray(c, "topic"),
		TimeFilter: logstore.TimeFilter{
			GTE: eventTimeFilter.GTE,
			LTE: eventTimeFilter.LTE,
			GT:  eventTimeFilter.GT,
			LT:  eventTimeFilter.LT,
		},
		Limit:     limit,
		Next:      c.Query("next"),
		Prev:      c.Query("prev"),
		SortOrder: dir,
	}

	response, err := h.logStore.ListEvent(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, cursor.ErrInvalidCursor) || errors.Is(err, cursor.ErrVersionMismatch) {
			AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
			return
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

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

	c.JSON(http.StatusOK, EventPaginatedResult{
		Models: apiEvents,
		Pagination: SeekPagination{
			OrderBy: orderBy,
			Dir:     dir,
			Limit:   limit,
			Next:    CursorToPtr(response.Next),
			Prev:    CursorToPtr(response.Prev),
		},
	})
}

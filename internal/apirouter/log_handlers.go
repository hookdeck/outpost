package apirouter

import (
	"net/http"
	"strconv"
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

// ExpandOptions represents which fields to expand in the response
type ExpandOptions struct {
	Event       bool
	EventData   bool
	Destination bool
}

func parseExpandOptions(c *gin.Context) ExpandOptions {
	opts := ExpandOptions{}
	for _, e := range c.QueryArray("expand") {
		switch e {
		case "event":
			opts.Event = true
		case "event.data":
			opts.Event = true
			opts.EventData = true
		case "destination":
			opts.Destination = true
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
func toAPIDelivery(de *models.DeliveryEvent, opts ExpandOptions) APIDelivery {
	api := APIDelivery{
		ID:          de.Delivery.ID,
		Attempt:     de.Attempt,
		Destination: de.DestinationID,
		Manual:      de.Manual,
	}

	// Set delivery fields if delivery exists
	if de.Delivery != nil {
		api.Status = de.Delivery.Status
		api.DeliveredAt = de.Delivery.Time
		api.Code = de.Delivery.Code
		api.ResponseData = de.Delivery.ResponseData
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
// Query params: event_id, destination_id, status, topic[], start, end, limit, next, prev, expand[]
func (h *LogHandlers) ListDeliveries(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}

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

	// Parse limit
	limit := 100
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Parse destination_id (single value for now)
	var destinationIDs []string
	if destID := c.Query("destination_id"); destID != "" {
		destinationIDs = []string{destID}
	}

	// Build request
	req := logstore.ListDeliveryEventRequest{
		TenantID:       tenant.ID,
		EventID:        c.Query("event_id"),
		DestinationIDs: destinationIDs,
		Status:         c.Query("status"),
		Topics:         c.QueryArray("topic"),
		DeliveryStart:  start,
		DeliveryEnd:    end,
		Limit:          limit,
		Next:           c.Query("next"),
		Prev:           c.Query("prev"),
	}

	// Call logstore
	response, err := h.logStore.ListDeliveryEvent(c.Request.Context(), req)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	// Parse expand options
	expandOpts := parseExpandOptions(c)

	// Transform to API response
	apiDeliveries := make([]APIDelivery, len(response.Data))
	for i, de := range response.Data {
		apiDeliveries[i] = toAPIDelivery(de, expandOpts)
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

	// Parse expand options
	expandOpts := parseExpandOptions(c)

	c.JSON(http.StatusOK, toAPIDelivery(deliveryEvent, expandOpts))
}

// ListEvents handles GET /:tenantID/events
// Query params: destination_id, topic[], start, end, limit, next, prev
func (h *LogHandlers) ListEvents(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}

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

	// Parse limit
	limit := 100
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Parse destination_id (single value for now)
	var destinationIDs []string
	if destID := c.Query("destination_id"); destID != "" {
		destinationIDs = []string{destID}
	}

	// Build request
	req := logstore.ListEventRequest{
		TenantID:       tenant.ID,
		DestinationIDs: destinationIDs,
		Topics:         c.QueryArray("topic"),
		EventStart:     start,
		EventEnd:       end,
		Limit:          limit,
		Next:           c.Query("next"),
		Prev:           c.Query("prev"),
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

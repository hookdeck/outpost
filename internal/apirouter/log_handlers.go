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
		case "response_data":
			opts.ResponseData = true
		}
	}
	return opts
}

// API Response types

// APIAttempt is the API response for an attempt
type APIAttempt struct {
	ID            string                 `json:"id"`
	Status        string                 `json:"status"`
	DeliveredAt   time.Time              `json:"delivered_at"`
	Code          string                 `json:"code,omitempty"`
	ResponseData  map[string]interface{} `json:"response_data,omitempty"`
	AttemptNumber int                    `json:"attempt_number"`
	Manual        bool                   `json:"manual"`

	EventID       string      `json:"event_id"`
	DestinationID string      `json:"destination_id"`
	Event         interface{} `json:"event,omitempty"`
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

// AttemptPaginatedResult is the paginated response for listing attempts.
type AttemptPaginatedResult struct {
	Models     []APIAttempt   `json:"models"`
	Pagination SeekPagination `json:"pagination"`
}

// EventPaginatedResult is the paginated response for listing events.
type EventPaginatedResult struct {
	Models     []APIEvent     `json:"models"`
	Pagination SeekPagination `json:"pagination"`
}

// toAPIAttempt converts an AttemptRecord to APIAttempt with expand options
func toAPIAttempt(ar *logstore.AttemptRecord, opts IncludeOptions) APIAttempt {
	api := APIAttempt{
		ID:            ar.Attempt.ID,
		Status:        ar.Attempt.Status,
		DeliveredAt:   ar.Attempt.Time,
		Code:          ar.Attempt.Code,
		AttemptNumber: ar.Attempt.AttemptNumber,
		Manual:        ar.Attempt.Manual,
		EventID:       ar.Attempt.EventID,
		DestinationID: ar.Attempt.DestinationID,
	}

	if opts.ResponseData {
		api.ResponseData = ar.Attempt.ResponseData
	}

	if ar.Event != nil {
		if opts.EventData {
			api.Event = APIEventFull{
				ID:               ar.Event.ID,
				Topic:            ar.Event.Topic,
				Time:             ar.Event.Time,
				EligibleForRetry: ar.Event.EligibleForRetry,
				Metadata:         ar.Event.Metadata,
				Data:             ar.Event.Data,
			}
		} else if opts.Event {
			api.Event = APIEventSummary{
				ID:               ar.Event.ID,
				Topic:            ar.Event.Topic,
				Time:             ar.Event.Time,
				EligibleForRetry: ar.Event.EligibleForRetry,
				Metadata:         ar.Event.Metadata,
			}
		}
	}

	return api
}

// ListAttempts handles GET /attempts
// Query params: tenant_id, event_id, destination_id, status, topic[], start, end, limit, next, prev, expand[], sort_order
func (h *LogHandlers) ListAttempts(c *gin.Context) {
	// Authz: JWT users can only query their own tenant's attempts
	tenantID, ok := resolveTenantIDFilter(c)
	if !ok {
		return
	}
	h.listAttemptsInternal(c, tenantID, "")
}

// ListDestinationAttempts handles GET /:tenant_id/destinations/:destination_id/attempts
// Same as ListAttempts but scoped to a specific destination via URL param.
func (h *LogHandlers) ListDestinationAttempts(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	destinationID := c.Param("destination_id")
	h.listAttemptsInternal(c, tenant.ID, destinationID)
}

func (h *LogHandlers) listAttemptsInternal(c *gin.Context, tenantID string, destinationID string) {
	// Parse and validate cursors (next/prev are mutually exclusive)
	cursors, errResp := ParseCursors(c)
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}

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
	attemptTimeFilter, errResp := ParseDateFilter(c, "time")
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}

	limit := parseLimit(c, 100, 1000)

	var destinationIDs []string
	if destinationID != "" {
		destinationIDs = []string{destinationID}
	} else if destID := c.Query("destination_id"); destID != "" {
		destinationIDs = []string{destID}
	}

	req := logstore.ListAttemptRequest{
		TenantID:       tenantID,
		EventID:        c.Query("event_id"),
		DestinationIDs: destinationIDs,
		Status:         c.Query("status"),
		Topics:         parseQueryArray(c, "topic"),
		TimeFilter: logstore.TimeFilter{
			GTE: attemptTimeFilter.GTE,
			LTE: attemptTimeFilter.LTE,
			GT:  attemptTimeFilter.GT,
			LT:  attemptTimeFilter.LT,
		},
		Limit:     limit,
		Next:      cursors.Next,
		Prev:      cursors.Prev,
		SortOrder: dir,
	}

	response, err := h.logStore.ListAttempt(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, cursor.ErrInvalidCursor) || errors.Is(err, cursor.ErrVersionMismatch) {
			AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
			return
		}
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	includeOpts := parseIncludeOptions(c)

	apiAttempts := make([]APIAttempt, len(response.Data))
	for i, ar := range response.Data {
		apiAttempts[i] = toAPIAttempt(ar, includeOpts)
	}

	c.JSON(http.StatusOK, AttemptPaginatedResult{
		Models: apiAttempts,
		Pagination: SeekPagination{
			OrderBy: orderBy,
			Dir:     dir,
			Limit:   limit,
			Next:    CursorToPtr(response.Next),
			Prev:    CursorToPtr(response.Prev),
		},
	})
}

// RetrieveEvent handles GET /events/:event_id
func (h *LogHandlers) RetrieveEvent(c *gin.Context) {
	// Authz: JWT users can only query their own tenant's events
	tenantID, ok := resolveTenantIDFilter(c)
	if !ok {
		return
	}
	eventID := c.Param("event_id")
	event, err := h.logStore.RetrieveEvent(c.Request.Context(), logstore.RetrieveEventRequest{
		TenantID: tenantID,
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

// RetrieveAttempt handles GET /attempts/:attempt_id
func (h *LogHandlers) RetrieveAttempt(c *gin.Context) {
	// Authz: JWT users can only query their own tenant's attempts
	tenantID, ok := resolveTenantIDFilter(c)
	if !ok {
		return
	}
	attemptID := c.Param("attempt_id")

	attemptRecord, err := h.logStore.RetrieveAttempt(c.Request.Context(), logstore.RetrieveAttemptRequest{
		TenantID:  tenantID,
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

	// Authz: when accessed via a destination-scoped route, verify the attempt
	// belongs to the destination in the path.
	if destinationID := c.Param("destination_id"); destinationID != "" {
		if attemptRecord.Attempt.DestinationID != destinationID {
			AbortWithError(c, http.StatusNotFound, NewErrNotFound("attempt"))
			return
		}
	}

	includeOpts := parseIncludeOptions(c)

	c.JSON(http.StatusOK, toAPIAttempt(attemptRecord, includeOpts))
}

// ListEvents handles GET /events
// Query params: tenant_id, destination_id, topic[], start, end, limit, next, prev, sort_order
func (h *LogHandlers) ListEvents(c *gin.Context) {
	// Authz: JWT users can only query their own tenant's events
	tenantID, ok := resolveTenantIDFilter(c)
	if !ok {
		return
	}
	h.listEventsInternal(c, tenantID)
}

func (h *LogHandlers) listEventsInternal(c *gin.Context, tenantID string) {
	// Parse and validate cursors (next/prev are mutually exclusive)
	cursors, errResp := ParseCursors(c)
	if errResp != nil {
		AbortWithError(c, errResp.Code, *errResp)
		return
	}

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
		Next:      cursors.Next,
		Prev:      cursors.Prev,
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

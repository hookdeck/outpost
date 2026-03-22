package apirouter

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/cursor"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/tenantstore"
)

type LogHandlers struct {
	logger      *logging.Logger
	logStore    logstore.LogStore
	tenantStore tenantstore.TenantStore
	displayer   destinationDisplayer
}

func NewLogHandlers(
	logger *logging.Logger,
	logStore logstore.LogStore,
	tenantStore tenantstore.TenantStore,
	displayer destinationDisplayer,
) *LogHandlers {
	return &LogHandlers{
		logger:      logger,
		logStore:    logStore,
		tenantStore: tenantStore,
		displayer:   displayer,
	}
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
	Destination  bool
}

func parseIncludeOptions(c *gin.Context) IncludeOptions {
	opts := IncludeOptions{}
	for _, e := range ParseArrayQueryParam(c, "include") {
		switch e {
		case "event":
			opts.Event = true
		case "event.data":
			opts.Event = true
			opts.EventData = true
		case "response_data":
			opts.ResponseData = true
		case "destination":
			opts.Destination = true
		}
	}
	return opts
}

// API Response types

// APIAttempt is the API response for an attempt
type APIAttempt struct {
	ID            string                 `json:"id"`
	TenantID      string                 `json:"tenant_id"`
	Status        string                 `json:"status"`
	Time          time.Time              `json:"time"`
	Code          string                 `json:"code,omitempty"`
	ResponseData  map[string]interface{} `json:"response_data,omitempty"`
	AttemptNumber int                    `json:"attempt_number"`
	Manual        bool                   `json:"manual"`

	EventID       string      `json:"event_id"`
	DestinationID string      `json:"destination_id"`
	Event         interface{} `json:"event,omitempty"`
	Destination   interface{} `json:"destination,omitempty"`
}

// APIEventSummary is the event object when expand=event (without data)
type APIEventSummary struct {
	ID               string            `json:"id"`
	TenantID         string            `json:"tenant_id"`
	DestinationID    string            `json:"destination_id"`
	Topic            string            `json:"topic"`
	Time             time.Time         `json:"time"`
	EligibleForRetry bool              `json:"eligible_for_retry"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// APIEventFull is the event object when expand=event.data
type APIEventFull struct {
	ID               string            `json:"id"`
	TenantID         string            `json:"tenant_id"`
	DestinationID    string            `json:"destination_id"`
	Topic            string            `json:"topic"`
	Time             time.Time         `json:"time"`
	EligibleForRetry bool              `json:"eligible_for_retry"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Data             json.RawMessage   `json:"data,omitempty"`
}

// APIEvent is the API response for retrieving a single event
type APIEvent struct {
	ID               string            `json:"id"`
	TenantID         string            `json:"tenant_id"`
	DestinationID    string            `json:"destination_id"`
	Topic            string            `json:"topic"`
	Time             time.Time         `json:"time"`
	EligibleForRetry bool              `json:"eligible_for_retry"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Data             json.RawMessage   `json:"data,omitempty"`
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

// toAPIAttempt converts an AttemptRecord to APIAttempt with expand options.
// destDisplay is optional; when non-nil and opts.Destination is true, the
// destination field is populated.
func toAPIAttempt(ar *logstore.AttemptRecord, opts IncludeOptions, destDisplay *destregistry.DestinationDisplay) APIAttempt {
	api := APIAttempt{
		ID:            ar.Attempt.ID,
		TenantID:      ar.Attempt.TenantID,
		Status:        ar.Attempt.Status,
		Time:          ar.Attempt.Time,
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
				TenantID:         ar.Event.TenantID,
				DestinationID:    ar.Event.DestinationID,
				Topic:            ar.Event.Topic,
				Time:             ar.Event.Time,
				EligibleForRetry: ar.Event.EligibleForRetry,
				Metadata:         ar.Event.Metadata,
				Data:             ar.Event.Data,
			}
		} else if opts.Event {
			api.Event = APIEventSummary{
				ID:               ar.Event.ID,
				TenantID:         ar.Event.TenantID,
				DestinationID:    ar.Event.DestinationID,
				Topic:            ar.Event.Topic,
				Time:             ar.Event.Time,
				EligibleForRetry: ar.Event.EligibleForRetry,
				Metadata:         ar.Event.Metadata,
			}
		}
	}

	if opts.Destination && destDisplay != nil {
		api.Destination = destDisplay
	}

	return api
}

// ListAttempts handles GET /attempts
// Query params: tenant_id[], event_id[], destination_id[], status, topic[], time[gte], time[lte], time[gt], time[lt], limit, next, prev, include, order_by, dir
func (h *LogHandlers) ListAttempts(c *gin.Context) {
	// Authz: JWT users can only query their own tenant's attempts
	tenantIDs, ok := resolveTenantIDsFilter(c)
	if !ok {
		return
	}
	h.listAttemptsInternal(c, tenantIDs, "")
}

// ListDestinationAttempts handles GET /:tenant_id/destinations/:destination_id/attempts
// Same as ListAttempts but scoped to a specific destination via URL param.
func (h *LogHandlers) ListDestinationAttempts(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	destinationID := c.Param("destination_id")
	h.listAttemptsInternal(c, []string{tenant.ID}, destinationID)
}

func (h *LogHandlers) listAttemptsInternal(c *gin.Context, tenantIDs []string, destinationID string) {
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
	} else {
		destinationIDs = ParseArrayQueryParam(c, "destination_id")
	}

	req := logstore.ListAttemptRequest{
		TenantIDs:      tenantIDs,
		EventIDs:       ParseArrayQueryParam(c, "event_id"),
		DestinationIDs: destinationIDs,
		Status:         c.Query("status"),
		Topics:         ParseArrayQueryParam(c, "topic"),
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

	// Batch-fetch destinations when include=destination is requested.
	destDisplayMap := map[string]*destregistry.DestinationDisplay{}
	if includeOpts.Destination {
		type destKey struct {
			tenantID, destinationID string
		}
		seen := map[destKey]struct{}{}
		for _, ar := range response.Data {
			k := destKey{ar.Attempt.TenantID, ar.Attempt.DestinationID}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			dest, err := h.tenantStore.RetrieveDestination(c.Request.Context(), k.tenantID, k.destinationID)
			if err != nil {
				// Deleted/not-found destinations are expected — skip silently.
				if errors.Is(err, tenantstore.ErrDestinationDeleted) || errors.Is(err, tenantstore.ErrDestinationNotFound) {
					continue
				}
				AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
				return
			}
			if dest == nil {
				continue
			}
			display, err := h.displayer.Display(dest)
			if err != nil {
				AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
				return
			}
			destDisplayMap[dest.ID] = display
		}
	}

	apiAttempts := make([]APIAttempt, len(response.Data))
	for i, ar := range response.Data {
		apiAttempts[i] = toAPIAttempt(ar, includeOpts, destDisplayMap[ar.Attempt.DestinationID])
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
	// When using JWT auth we need to inject the tenant ID for access control in case the id doesn't belong to the tenant
	ctxTenantID := tenantIDFromContext(c)
	eventID := c.Param("event_id")
	event, err := h.logStore.RetrieveEvent(c.Request.Context(), logstore.RetrieveEventRequest{
		TenantID: ctxTenantID,
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
		TenantID:         event.TenantID,
		DestinationID:    event.DestinationID,
		Topic:            event.Topic,
		Time:             event.Time,
		EligibleForRetry: event.EligibleForRetry,
		Metadata:         event.Metadata,
		Data:             event.Data,
	})
}

// RetrieveAttempt handles GET /attempts/:attempt_id
func (h *LogHandlers) RetrieveAttempt(c *gin.Context) {
	// When using JWT auth we need to inject the tenant ID for access control in case the id doesn't belong to the tenant
	ctxTenantID := tenantIDFromContext(c)
	attemptID := c.Param("attempt_id")

	attemptRecord, err := h.logStore.RetrieveAttempt(c.Request.Context(), logstore.RetrieveAttemptRequest{
		TenantID:  ctxTenantID,
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

	var destDisplay *destregistry.DestinationDisplay
	if includeOpts.Destination {
		dest, err := h.tenantStore.RetrieveDestination(c.Request.Context(), attemptRecord.Attempt.TenantID, attemptRecord.Attempt.DestinationID)
		if err != nil && !errors.Is(err, tenantstore.ErrDestinationDeleted) && !errors.Is(err, tenantstore.ErrDestinationNotFound) {
			AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
			return
		}
		if err == nil && dest != nil {
			display, err := h.displayer.Display(dest)
			if err != nil {
				AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
				return
			}
			destDisplay = display
		}
	}

	c.JSON(http.StatusOK, toAPIAttempt(attemptRecord, includeOpts, destDisplay))
}

// ListEvents handles GET /events
// Query params: tenant_id[], id[], destination_id, topic[], time[gte], time[lte], time[gt], time[lt], limit, next, prev, order_by, dir
func (h *LogHandlers) ListEvents(c *gin.Context) {
	// Authz: JWT users can only query their own tenant's events
	tenantIDs, ok := resolveTenantIDsFilter(c)
	if !ok {
		return
	}
	h.listEventsInternal(c, tenantIDs)
}

func (h *LogHandlers) listEventsInternal(c *gin.Context, tenantIDs []string) {
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
		TenantIDs:      tenantIDs,
		EventIDs:       ParseArrayQueryParam(c, "id"),
		DestinationIDs: destinationIDs,
		Topics:         ParseArrayQueryParam(c, "topic"),
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
			TenantID:         e.TenantID,
			DestinationID:    e.DestinationID,
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

package api

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

func (h *LogHandlers) ListEvent(c *gin.Context) {
	h.listEvent(c, c.QueryArray("destination_id"))
}

func (h *LogHandlers) ListEventByDestination(c *gin.Context) {
	h.listEvent(c, []string{c.Param("destinationID")})
}

func (h *LogHandlers) listEvent(c *gin.Context, destinationIDs []string) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}

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

	limitStr := c.Query("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 100
	}
	response, err := h.logStore.ListEvent(c.Request.Context(), logstore.ListEventRequest{
		Next:           c.Query("next"),
		Prev:           c.Query("prev"),
		Limit:          limit,
		Start:          start,
		End:            end,
		TenantID:       tenant.ID,
		DestinationIDs: destinationIDs,
		Topics:         c.QueryArray("topic"),
		Status:         c.Query("status"),
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if len(response.Data) == 0 {
		// Return an empty array instead of null
		c.JSON(http.StatusOK, gin.H{
			"data":  []models.Event{},
			"next":  "",
			"prev":  "",
			"count": 0,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":  response.Data,
		"next":  response.Next,
		"prev":  response.Prev,
		"count": response.Count,
	})
}

func (h *LogHandlers) RetrieveEvent(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	eventID := c.Param("eventID")
	event, err := h.logStore.RetrieveEvent(c.Request.Context(), tenant.ID, eventID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if event == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, event)
}

func (h *LogHandlers) RetrieveEventByDestination(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	destinationID := c.Param("destinationID")
	eventID := c.Param("eventID")
	event, err := h.logStore.RetrieveEventByDestination(c.Request.Context(), tenant.ID, destinationID, eventID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if event == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, event)
}

type DeliveryResponse struct {
	ID           string                 `json:"id"`
	DeliveredAt  string                 `json:"delivered_at"`
	Status       string                 `json:"status"`
	Code         string                 `json:"code"`
	ResponseData map[string]interface{} `json:"response_data"`
}

func (h *LogHandlers) ListDeliveryByEvent(c *gin.Context) {
	event := h.mustEventWithTenant(c, c.Param("eventID"))
	if event == nil {
		return
	}
	deliveries, err := h.logStore.ListDelivery(c.Request.Context(), logstore.ListDeliveryRequest{
		EventID:       event.ID,
		DestinationID: c.Query("destination_id"),
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if len(deliveries) == 0 {
		// Return an empty array instead of null
		c.JSON(http.StatusOK, []DeliveryResponse{})
		return
	}
	deliveryData := make([]DeliveryResponse, len(deliveries))
	for i, delivery := range deliveries {
		deliveryData[i] = DeliveryResponse{
			ID:           delivery.ID,
			DeliveredAt:  delivery.Time.UTC().Format(time.RFC3339),
			Status:       delivery.Status,
			Code:         delivery.Code,
			ResponseData: delivery.ResponseData,
		}
	}
	c.JSON(http.StatusOK, deliveryData)
}

func (h *LogHandlers) mustEventWithTenant(c *gin.Context, eventID string) *models.Event {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return nil
	}
	event, err := h.logStore.RetrieveEvent(c.Request.Context(), tenant.ID, eventID)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return nil
	}
	if event == nil {
		c.Status(http.StatusNotFound)
		return nil
	}
	if event.TenantID != tenant.ID {
		c.Status(http.StatusForbidden)
		return nil
	}
	return event
}

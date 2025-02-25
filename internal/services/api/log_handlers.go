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
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	cursor := c.Query("cursor")
	limitStr := c.Query("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 100
	}
	events, nextCursor, err := h.logStore.ListEvent(c.Request.Context(), logstore.ListEventRequest{
		TenantID: tenant.ID,
		Cursor:   cursor,
		Limit:    limit,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if len(events) == 0 {
		// Return an empty array instead of null
		c.JSON(http.StatusOK, gin.H{
			"data": []models.Event{},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": events,
		"next": nextCursor,
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

type DeliveryResponse struct {
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
		EventID: event.ID,
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

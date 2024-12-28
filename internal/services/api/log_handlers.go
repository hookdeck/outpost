package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

type LogHandlers struct {
	logger   *otelzap.Logger
	logStore models.LogStore
}

func NewLogHandlers(
	logger *otelzap.Logger,
	logStore models.LogStore,
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
	events, nextCursor, err := h.logStore.ListEvent(c.Request.Context(), models.ListEventRequest{
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

// TODO: consider authz where eventID doesn't belong to tenantID?
func (h *LogHandlers) ListDeliveryByEvent(c *gin.Context) {
	tenant := mustTenantFromContext(c)
	if tenant == nil {
		return
	}
	eventID := c.Param("eventID")
	deliveries, err := h.logStore.ListDelivery(c.Request.Context(), models.ListDeliveryRequest{
		EventID: eventID,
	})
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	if len(deliveries) == 0 {
		// Return an empty array instead of null
		c.JSON(http.StatusOK, gin.H{
			"data": []models.Delivery{},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": deliveries,
	})
}

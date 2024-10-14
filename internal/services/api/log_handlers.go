package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
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
		h.logger.Ctx(c.Request.Context()).Error("failed to list events", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list events"})
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

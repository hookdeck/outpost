package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type PublishHandlers struct {
	logger       *otelzap.Logger
	eventHandler publishmq.EventHandler
}

func NewPublishHandlers(
	logger *otelzap.Logger,
	eventHandler publishmq.EventHandler,
) *PublishHandlers {
	return &PublishHandlers{
		logger:       logger,
		eventHandler: eventHandler,
	}
}

func (h *PublishHandlers) Ingest(c *gin.Context) {
	logger := h.logger.Ctx(c.Request.Context())
	var publishedEvent PublishedEvent
	if err := c.ShouldBindJSON(&publishedEvent); err != nil {
		logger.Error("failed to bind JSON", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	event := publishedEvent.toEvent()
	if err := h.eventHandler.Handle(c.Request.Context(), &event); err != nil {
		logger.Error("failed to ingest event", zap.Error(err))
		if errors.Is(err, idempotence.ErrConflict) {
			c.Status(http.StatusConflict)
		} else if errors.Is(err, publishmq.ErrInvalidTopic) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to ingest event"})
		}
		return
	}
	c.Status(http.StatusOK)
}

// TODO: validation
type PublishedEvent struct {
	ID               string                 `json:"id"`
	TenantID         string                 `json:"tenant_id"`
	DestinationID    string                 `json:"destination_id"`
	Topic            string                 `json:"topic"`
	EligibleForRetry bool                   `json:"eligible_for_retry"`
	Time             time.Time              `json:"time"`
	Metadata         map[string]string      `json:"metadata"`
	Data             map[string]interface{} `json:"data"`
}

func (p *PublishedEvent) toEvent() models.Event {
	id := p.ID
	if id == "" {
		id = uuid.New().String()
	}
	eventTime := p.Time
	if eventTime.IsZero() {
		eventTime = time.Now()
	}
	return models.Event{
		ID:               id,
		TenantID:         p.TenantID,
		DestinationID:    p.DestinationID,
		Topic:            p.Topic,
		EligibleForRetry: p.EligibleForRetry,
		Time:             eventTime,
		Metadata:         p.Metadata,
		Data:             p.Data,
	}
}

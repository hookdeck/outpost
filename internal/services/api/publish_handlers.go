package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/idempotence"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/publishmq"
)

type PublishHandlers struct {
	logger       *logging.Logger
	eventHandler publishmq.EventHandler
}

func NewPublishHandlers(
	logger *logging.Logger,
	eventHandler publishmq.EventHandler,
) *PublishHandlers {
	return &PublishHandlers{
		logger:       logger,
		eventHandler: eventHandler,
	}
}

func (h *PublishHandlers) Ingest(c *gin.Context) {
	var publishedEvent PublishedEvent
	if err := c.ShouldBindJSON(&publishedEvent); err != nil {
		AbortWithValidationError(c, err)
		return
	}
	event := publishedEvent.toEvent()
	if err := h.eventHandler.Handle(c.Request.Context(), &event); err != nil {
		if errors.Is(err, idempotence.ErrConflict) {
			c.Status(http.StatusConflict)
		} else if errors.Is(err, publishmq.ErrRequiredTopic) {
			AbortWithValidationError(c, ErrorResponse{
				Code:    http.StatusUnprocessableEntity,
				Message: "validation error",
				Err:     err,
				Data: map[string]string{
					"topic": "required",
				},
			})
		} else if errors.Is(err, publishmq.ErrInvalidTopic) {
			AbortWithValidationError(c, ErrorResponse{
				Code:    http.StatusUnprocessableEntity,
				Message: "validation error",
				Err:     err,
				Data: map[string]string{
					"topic": "invalid",
				},
			})
		} else {
			AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		}
		return
	}
	c.Status(http.StatusOK)
}

type PublishedEvent struct {
	ID               string                 `json:"id"`
	TenantID         string                 `json:"tenant_id" binding:"required"`
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

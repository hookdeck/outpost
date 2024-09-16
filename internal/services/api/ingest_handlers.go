package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type IngestHandlers struct {
	logger      *otelzap.Logger
	redisClient *redis.Client
	ingestor    *ingest.Ingestor
}

func NewIngestHandlers(
	logger *otelzap.Logger,
	redisClient *redis.Client,
	ingestor *ingest.Ingestor,
) *IngestHandlers {
	return &IngestHandlers{
		logger:      logger,
		redisClient: redisClient,
		ingestor:    ingestor,
	}
}

func (h *IngestHandlers) Ingest(c *gin.Context) {
	var publishedEvent PublishedEvent
	if err := c.ShouldBindJSON(&publishedEvent); err != nil {
		h.logger.Error("failed to bind JSON", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.ingestor.Publish(c.Request.Context(), publishedEvent.toEvent())
	if err != nil {
		h.logger.Error("failed to ingest event", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to ingest event"})
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

func (p *PublishedEvent) toEvent() ingest.Event {
	id := p.ID
	if id == "" {
		id = uuid.New().String()
	}
	return ingest.Event{
		ID:               id,
		TenantID:         p.TenantID,
		DestinationID:    p.DestinationID,
		Topic:            p.Topic,
		EligibleForRetry: p.EligibleForRetry,
		Time:             p.Time,
		Metadata:         p.Metadata,
		Data:             p.Data,
	}
}

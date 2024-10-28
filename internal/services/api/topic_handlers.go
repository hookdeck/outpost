package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
)

type TopicHandlers struct {
	logger *otelzap.Logger
	topics []string
}

func NewTopicHandlers(logger *otelzap.Logger, topics []string) *TopicHandlers {
	return &TopicHandlers{
		logger: logger,
		topics: topics,
	}
}

func (h *TopicHandlers) List(c *gin.Context) {
	c.JSON(http.StatusOK, h.topics)
}

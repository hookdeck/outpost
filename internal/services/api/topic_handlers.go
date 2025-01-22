package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/logging"
)

type TopicHandlers struct {
	logger *logging.Logger
	topics []string
}

func NewTopicHandlers(logger *logging.Logger, topics []string) *TopicHandlers {
	return &TopicHandlers{
		logger: logger,
		topics: topics,
	}
}

func (h *TopicHandlers) List(c *gin.Context) {
	c.JSON(http.StatusOK, h.topics)
}

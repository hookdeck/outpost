package apirouter

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/topicschema"
)

// jsonContentType is the Content-Type gin sets for c.JSON, used for bodies
// encoded ahead of time.
const jsonContentType = "application/json; charset=utf-8"

type TopicHandlers struct {
	logger *logging.Logger
	topics []string
	// topicObjects is the v2 body, encoded once: the catalog is immutable, so
	// every request is served the same bytes.
	topicObjects []byte
}

// NewTopicHandlers serves topics as names in v1 and as the catalog's topic
// objects in v2.
func NewTopicHandlers(logger *logging.Logger, topics []string, catalog *topicschema.Catalog) (*TopicHandlers, error) {
	if topics == nil {
		topics = []string{}
	}
	topicObjects, err := json.Marshal(catalog.Topics())
	if err != nil {
		return nil, fmt.Errorf("encode topics: %w", err)
	}
	return &TopicHandlers{
		logger:       logger,
		topics:       topics,
		topicObjects: topicObjects,
	}, nil
}

func (h *TopicHandlers) List(c *gin.Context) {
	if apiVersionFromContext(c) >= apiV2 {
		c.Data(http.StatusOK, jsonContentType, h.topicObjects)
		return
	}
	c.JSON(http.StatusOK, h.topics)
}

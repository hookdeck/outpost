package apirouter

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/topicschema"
)

const (
	defaultMCPEventsLimit = 50
	maxMCPEventsLimit     = 100
)

type MCPEventsHandlers struct {
	logger  *logging.Logger
	catalog *topicschema.Catalog
}

func NewMCPEventsHandlers(logger *logging.Logger, catalog *topicschema.Catalog) *MCPEventsHandlers {
	return &MCPEventsHandlers{logger: logger, catalog: catalog}
}

type mcpEventsResponse struct {
	ResultType string            `json:"resultType"`
	Events     []json.RawMessage `json:"events"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

func (h *MCPEventsHandlers) List(c *gin.Context) {
	if h.catalog == nil {
		AbortWithError(c, http.StatusNotFound, ErrorResponse{Code: http.StatusNotFound, Message: "not found"})
		return
	}
	limit, errResp := ParseLimit(c, maxMCPEventsLimit)
	if errResp != nil {
		AbortWithError(c, http.StatusBadRequest, *errResp)
		return
	}
	if limit == 0 {
		limit = defaultMCPEventsLimit
	}
	after, err := decodeMCPEventsCursor(c.Query("cursor"))
	if err != nil {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
		return
	}

	var allowed map[string]bool
	if values, ok := c.GetQueryArray("topics"); ok {
		allowed = map[string]bool{}
		for _, value := range values {
			for _, name := range strings.Split(value, ",") {
				if name = strings.TrimSpace(name); name != "" {
					allowed[name] = true
				}
			}
		}
	}

	resp := mcpEventsResponse{ResultType: "complete", Events: []json.RawMessage{}}
	last := ""
	for _, ev := range h.catalog.Events() {
		if after != "" && ev.Name <= after {
			continue
		}
		if allowed != nil && !allowed[ev.Name] {
			continue
		}
		if len(resp.Events) == limit {
			resp.NextCursor = encodeMCPEventsCursor(last)
			break
		}
		resp.Events = append(resp.Events, ev.JSON)
		last = ev.Name
	}
	c.JSON(http.StatusOK, resp)
}

func encodeMCPEventsCursor(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

func decodeMCPEventsCursor(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return "", errors.New("invalid cursor")
	}
	return string(b), nil
}

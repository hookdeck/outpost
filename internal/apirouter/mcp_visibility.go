package apirouter

import (
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
)

// API v1 predates MCP Events: its clients can't render mcp destinations, so
// v1 leaves them, and their attempts, out of every response.

// v1HiddenDestinationTypes are the destination types API v1 never returns.
var v1HiddenDestinationTypes = []string{models.DestinationTypeMCP}

// hiddenInRequest reports whether the request's API version hides
// destinations of type typ.
func hiddenInRequest(c *gin.Context, typ string) bool {
	return apiVersionFromContext(c) < apiV2 && slices.Contains(v1HiddenDestinationTypes, typ)
}

// v1AttemptTypes restricts v1 attempt listings to the visible destination
// types. It is an include list (the registered types minus the hidden ones)
// rather than an exclusion, so the log stores can use their destination type
// indexes.
type v1AttemptTypes struct {
	// active is set when a hidden type is registered; otherwise no attempt
	// of a hidden type can exist and listings are left alone.
	active  bool
	visible []string
}

func newV1AttemptTypes(registry destregistry.Registry) v1AttemptTypes {
	var t v1AttemptTypes
	for _, meta := range registry.ListProviderMetadata() {
		if meta == nil {
			continue
		}
		if slices.Contains(v1HiddenDestinationTypes, meta.Type) {
			t.active = true
			continue
		}
		t.visible = append(t.visible, meta.Type)
	}
	return t
}

// filter returns the destination types a v1 listing queries for the
// requested ones (none requested: every type), and false when no visible
// type is left, so the listing is empty.
func (t v1AttemptTypes) filter(requested []string) ([]string, bool) {
	if !t.active {
		return requested, true
	}
	if len(requested) == 0 {
		return t.visible, len(t.visible) > 0
	}
	visible := slices.DeleteFunc(slices.Clone(requested), func(typ string) bool {
		return slices.Contains(v1HiddenDestinationTypes, typ)
	})
	return visible, len(visible) > 0
}

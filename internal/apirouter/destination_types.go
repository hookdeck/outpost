package apirouter

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/topicschema"
)

// Destination type create modes, v2 only.
const (
	// createModeForm: create from a form of config_fields and
	// credential_fields, submitted to POST .../destinations.
	createModeForm = "form"
	// createModeExternal: created elsewhere (mcp: by agents); clients show
	// instructions instead of a form.
	createModeExternal = "external"
)

// destinationTypeV1 is a destination type as API v1 returns it: the provider
// metadata as is. CreateMode shadows a create_mode the metadata may carry, so
// v1 never returns it.
type destinationTypeV1 struct {
	*metadata.ProviderMetadata
	CreateMode *struct{} `json:"create_mode,omitempty"`
}

// destinationTypeV2 is a destination type as API v2 returns it: the
// provider metadata with create_mode, and instructions rendered for the mcp
// type. Both shadow the fields of the shared metadata, which is never
// modified.
type destinationTypeV2 struct {
	*metadata.ProviderMetadata
	CreateMode   string `json:"create_mode"`
	Instructions string `json:"instructions"`
}

// destinationTypes serves GET /destination-types[/:type]. The registry and
// the catalog don't change after startup, so the lists are encoded once.
type destinationTypes struct {
	registry destregistry.Registry
	// mcpVisible: v2 lists the mcp type, which needs an MCP-enabled topic.
	mcpVisible bool
	v1List     []byte
	v2List     []byte
	// v2ByType holds the v2 encoding of every listed type.
	v2ByType map[string][]byte
	// mcpInstructions is the rendered instructions of the mcp type.
	mcpInstructions string
}

func newDestinationTypes(registry destregistry.Registry, catalog *topicschema.Catalog, serverURL string) (*destinationTypes, error) {
	t := &destinationTypes{
		registry:   registry,
		mcpVisible: catalog.MCPEnabled(),
		v2ByType:   map[string][]byte{},
	}
	all := registry.ListProviderMetadata()
	v1 := make([]destinationTypeV1, 0, len(all))
	v2 := make([]destinationTypeV2, 0, len(all))
	for _, meta := range all {
		if meta == nil {
			continue
		}
		if meta.Type == models.DestinationTypeMCP {
			// The mcp instructions are a Go text/template (see the
			// provider's instructions.md); values are rendered as code
			// spans, so they can't break the markdown.
			instructions, err := destmcp.RenderInstructions(meta.Instructions, destmcp.InstructionsData{
				ServerURL: serverURL,
				Topics:    catalog.MCPTopics(),
			})
			if err != nil {
				return nil, err
			}
			t.mcpInstructions = instructions
		} else {
			v1 = append(v1, destinationTypeV1{ProviderMetadata: meta})
		}
		if meta.Type == models.DestinationTypeMCP && !t.mcpVisible {
			continue
		}
		dto, err := t.v2(meta)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(dto)
		if err != nil {
			return nil, fmt.Errorf("encode destination type %s: %w", meta.Type, err)
		}
		t.v2ByType[meta.Type] = encoded
		v2 = append(v2, dto)
	}
	var err error
	if t.v1List, err = json.Marshal(v1); err != nil {
		return nil, fmt.Errorf("encode destination types: %w", err)
	}
	if t.v2List, err = json.Marshal(v2); err != nil {
		return nil, fmt.Errorf("encode destination types: %w", err)
	}
	return t, nil
}

// v2 builds the v2 form of a type.
func (t *destinationTypes) v2(meta *metadata.ProviderMetadata) (destinationTypeV2, error) {
	mode, err := createMode(meta)
	if err != nil {
		return destinationTypeV2{}, err
	}
	instructions := meta.Instructions
	if meta.Type == models.DestinationTypeMCP {
		instructions = t.mcpInstructions
	}
	return destinationTypeV2{ProviderMetadata: meta, CreateMode: mode, Instructions: instructions}, nil
}

// createMode reads the create_mode of a provider's metadata: form unless it
// says otherwise. mcp is external even when its metadata doesn't say.
func createMode(meta *metadata.ProviderMetadata) (string, error) {
	encoded, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("encode destination type %s: %w", meta.Type, err)
	}
	var probe struct {
		CreateMode string `json:"create_mode"`
	}
	if err := json.Unmarshal(encoded, &probe); err != nil {
		return "", fmt.Errorf("decode destination type %s: %w", meta.Type, err)
	}
	switch {
	case probe.CreateMode != "":
		return probe.CreateMode, nil
	case meta.Type == models.DestinationTypeMCP:
		return createModeExternal, nil
	}
	return createModeForm, nil
}

func (t *destinationTypes) list(c *gin.Context) {
	if apiVersionFromContext(c) >= apiV2 {
		c.Data(http.StatusOK, jsonContentType, t.v2List)
		return
	}
	c.Data(http.StatusOK, jsonContentType, t.v1List)
}

func (t *destinationTypes) retrieve(c *gin.Context) {
	providerType := c.Param("type")
	v2 := apiVersionFromContext(c) >= apiV2
	if providerType == models.DestinationTypeMCP && (!v2 || !t.mcpVisible) {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("destination type"))
		return
	}
	meta, err := t.registry.RetrieveProviderMetadata(providerType)
	if err != nil || meta == nil {
		AbortWithError(c, http.StatusNotFound, NewErrNotFound("destination type"))
		return
	}
	if !v2 {
		c.JSON(http.StatusOK, destinationTypeV1{ProviderMetadata: meta})
		return
	}
	if encoded, ok := t.v2ByType[providerType]; ok {
		c.Data(http.StatusOK, jsonContentType, encoded)
		return
	}
	dto, err := t.v2(meta)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}
	c.JSON(http.StatusOK, dto)
}

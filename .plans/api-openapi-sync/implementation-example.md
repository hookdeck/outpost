# Implementation Example: Schema Generation Tool

## Sample Schema Generator

```go
// cmd/schema-gen/main.go
package main

import (
    "encoding/json"
    "fmt"
    "reflect"
    "strings"
    
    "github.com/hookdeck/outpost/internal/services/api"
    "github.com/hookdeck/outpost/internal/models"
    "gopkg.in/yaml.v3"
)

type OpenAPISchema struct {
    Type       string                    `yaml:"type,omitempty"`
    Properties map[string]*OpenAPISchema `yaml:"properties,omitempty"`
    Required   []string                  `yaml:"required,omitempty"`
    Items      *OpenAPISchema           `yaml:"items,omitempty"`
    Example    interface{}              `yaml:"example,omitempty"`
}

func main() {
    schemas := make(map[string]*OpenAPISchema)
    
    // Generate schemas for API request/response types
    schemas["CreateDestinationRequest"] = generateSchema(api.CreateDestinationRequest{})
    schemas["UpdateDestinationRequest"] = generateSchema(api.UpdateDestinationRequest{})
    schemas["PublishedEvent"] = generateSchema(api.PublishedEvent{})
    
    // Generate schemas for models
    schemas["Destination"] = generateSchema(models.Destination{})
    schemas["Event"] = generateSchema(models.Event{})
    schemas["Tenant"] = generateSchema(models.Tenant{})
    
    // Output as YAML
    output := map[string]interface{}{
        "components": map[string]interface{}{
            "schemas": schemas,
        },
    }
    
    data, _ := yaml.Marshal(output)
    fmt.Println(string(data))
}

func generateSchema(v interface{}) *OpenAPISchema {
    t := reflect.TypeOf(v)
    return generateSchemaFromType(t)
}

func generateSchemaFromType(t reflect.Type) *OpenAPISchema {
    schema := &OpenAPISchema{}
    
    switch t.Kind() {
    case reflect.Struct:
        schema.Type = "object"
        schema.Properties = make(map[string]*OpenAPISchema)
        
        for i := 0; i < t.NumField(); i++ {
            field := t.Field(i)
            jsonTag := field.Tag.Get("json")
            
            if jsonTag == "-" {
                continue
            }
            
            fieldName := strings.Split(jsonTag, ",")[0]
            if fieldName == "" {
                fieldName = field.Name
            }
            
            // Check if field is required
            bindingTag := field.Tag.Get("binding")
            if strings.Contains(bindingTag, "required") {
                schema.Required = append(schema.Required, fieldName)
            }
            
            // Generate schema for field
            fieldSchema := generateSchemaFromType(field.Type)
            
            // Add OpenAPI metadata from tags
            if desc := field.Tag.Get("openapi:description"); desc != "" {
                fieldSchema.Description = desc
            }
            if example := field.Tag.Get("openapi:example"); example != "" {
                fieldSchema.Example = example
            }
            
            schema.Properties[fieldName] = fieldSchema
        }
        
    case reflect.String:
        schema.Type = "string"
        
    case reflect.Int, reflect.Int32, reflect.Int64:
        schema.Type = "integer"
        
    case reflect.Bool:
        schema.Type = "boolean"
        
    case reflect.Slice:
        schema.Type = "array"
        schema.Items = generateSchemaFromType(t.Elem())
        
    case reflect.Map:
        schema.Type = "object"
        
    case reflect.Ptr:
        return generateSchemaFromType(t.Elem())
    }
    
    return schema
}
```

## Provider Schema Generator

```go
// cmd/provider-doc-gen/main.go
package main

import (
    "fmt"
    "github.com/hookdeck/outpost/internal/destregistry"
    "github.com/hookdeck/outpost/internal/destregistry/metadata"
    "gopkg.in/yaml.v3"
)

func main() {
    registry := initializeRegistry()
    schemas := make(map[string]interface{})
    
    // Generate schemas for each provider
    for _, provider := range registry.ListProviderMetadata() {
        generateProviderSchemas(provider, schemas)
    }
    
    // Output as YAML
    output := map[string]interface{}{
        "components": map[string]interface{}{
            "schemas": schemas,
        },
    }
    
    data, _ := yaml.Marshal(output)
    fmt.Println(string(data))
}

func generateProviderSchemas(meta *metadata.ProviderMetadata, schemas map[string]interface{}) {
    // Config schema
    configSchema := map[string]interface{}{
        "type":       "object",
        "properties": make(map[string]interface{}),
        "required":   []string{},
    }
    
    for _, field := range meta.ConfigFields {
        fieldSchema := generateFieldSchema(field)
        configSchema["properties"].(map[string]interface{})[field.Key] = fieldSchema
        
        if field.Required {
            configSchema["required"] = append(
                configSchema["required"].([]string), 
                field.Key,
            )
        }
    }
    
    schemas[fmt.Sprintf("%sConfig", meta.Type)] = configSchema
    
    // Credentials schema
    credSchema := map[string]interface{}{
        "type":       "object",
        "properties": make(map[string]interface{}),
        "required":   []string{},
    }
    
    for _, field := range meta.CredentialFields {
        fieldSchema := generateFieldSchema(field)
        credSchema["properties"].(map[string]interface{})[field.Key] = fieldSchema
        
        if field.Required {
            credSchema["required"] = append(
                credSchema["required"].([]string),
                field.Key,
            )
        }
    }
    
    schemas[fmt.Sprintf("%sCredentials", meta.Type)] = credSchema
}

func generateFieldSchema(field metadata.FieldSchema) map[string]interface{} {
    schema := map[string]interface{}{
        "type":        field.Type,
        "description": field.Description,
    }
    
    if field.Pattern != nil {
        schema["pattern"] = *field.Pattern
    }
    
    if field.MinLength != nil {
        schema["minLength"] = *field.MinLength
    }
    
    if field.MaxLength != nil {
        schema["maxLength"] = *field.MaxLength
    }
    
    if field.Example != "" {
        schema["example"] = field.Example
    }
    
    if field.Sensitive {
        schema["x-sensitive"] = true
    }
    
    return schema
}
```

## Validation Script

```go
// cmd/validate-api-docs/main.go
package main

import (
    "encoding/json"
    "fmt"
    "net/http/httptest"
    "os"
    
    "github.com/gin-gonic/gin"
    "github.com/getkin/kin-openapi/openapi3"
    "github.com/hookdeck/outpost/internal/services/api"
)

func main() {
    // Load OpenAPI spec
    loader := &openapi3.Loader{Context: context.Background()}
    doc, err := loader.LoadFromFile("docs/apis/openapi.yaml")
    if err != nil {
        fmt.Printf("Failed to load OpenAPI spec: %v\n", err)
        os.Exit(1)
    }
    
    // Initialize router
    router := initializeTestRouter()
    
    // Validate each path
    hasErrors := false
    for path, pathItem := range doc.Paths {
        for method, operation := range pathItem.Operations() {
            if err := validateEndpoint(router, path, method, operation); err != nil {
                fmt.Printf("❌ %s %s: %v\n", method, path, err)
                hasErrors = true
            } else {
                fmt.Printf("✅ %s %s\n", method, path)
            }
        }
    }
    
    if hasErrors {
        os.Exit(1)
    }
}

func validateEndpoint(router *gin.Engine, path string, method string, operation *openapi3.Operation) error {
    // Create test request
    w := httptest.NewRecorder()
    req := httptest.NewRequest(method, path, nil)
    
    // Add required auth header if needed
    if operation.Security != nil {
        req.Header.Set("Authorization", "Bearer test-token")
    }
    
    // Execute request
    router.ServeHTTP(w, req)
    
    // Check if endpoint exists
    if w.Code == 404 {
        return fmt.Errorf("endpoint not found in implementation")
    }
    
    // Validate response schema
    if operation.Responses != nil {
        statusStr := fmt.Sprintf("%d", w.Code)
        response := operation.Responses[statusStr]
        
        if response == nil && operation.Responses["default"] != nil {
            response = operation.Responses["default"]
        }
        
        if response != nil && response.Value != nil {
            // Validate response body against schema
            if err := validateResponseBody(w.Body.Bytes(), response.Value); err != nil {
                return fmt.Errorf("response validation failed: %w", err)
            }
        }
    }
    
    return nil
}

func validateResponseBody(body []byte, response *openapi3.Response) error {
    if len(body) == 0 {
        return nil
    }
    
    var data interface{}
    if err := json.Unmarshal(body, &data); err != nil {
        return fmt.Errorf("invalid JSON response: %w", err)
    }
    
    // Further validation against schema
    // This would use the response schema definition
    
    return nil
}
```

## Makefile Integration

```makefile
# Makefile additions

.PHONY: generate-schemas
generate-schemas:
	@echo "Generating schemas from Go structs..."
	@go run cmd/schema-gen/main.go > .tmp/schemas.yaml
	@echo "Generating provider schemas..."
	@go run cmd/provider-doc-gen/main.go > .tmp/providers.yaml
	@echo "Merging schemas..."
	@yq eval-all 'select(fileIndex == 0) * select(fileIndex == 1)' \
		docs/apis/openapi-base.yaml .tmp/schemas.yaml > .tmp/openapi-with-schemas.yaml
	@yq eval-all 'select(fileIndex == 0) * select(fileIndex == 1)' \
		.tmp/openapi-with-schemas.yaml .tmp/providers.yaml > docs/apis/openapi.yaml
	@echo "OpenAPI documentation updated!"

.PHONY: validate-api-docs
validate-api-docs:
	@echo "Validating API documentation..."
	@go run cmd/validate-api-docs/main.go

.PHONY: check-api-sync
check-api-sync: generate-schemas
	@echo "Checking if API documentation is in sync..."
	@git diff --exit-code docs/apis/openapi.yaml || \
		(echo "API documentation is out of sync! Run 'make generate-schemas' and commit changes." && exit 1)

# Add to CI targets
ci-api-docs: check-api-sync validate-api-docs
```

## GitHub Actions Workflow

```yaml
# .github/workflows/api-docs.yml
name: API Documentation

on:
  push:
    paths:
      - 'internal/services/api/**'
      - 'internal/destregistry/**'
      - 'internal/models/**'
      - 'docs/apis/**'
      - '.github/workflows/api-docs.yml'
  pull_request:
    paths:
      - 'internal/services/api/**'
      - 'internal/destregistry/**'
      - 'internal/models/**'
      - 'docs/apis/**'

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Install tools
        run: |
          go install github.com/mikefarah/yq/v4@latest
          npm install -g @redocly/cli @stoplight/spectral-cli
      
      - name: Generate schemas
        run: make generate-schemas
      
      - name: Check sync
        run: |
          git diff --exit-code docs/apis/openapi.yaml || {
            echo "::error::API documentation is out of sync"
            echo "Run 'make generate-schemas' locally and commit the changes"
            exit 1
          }
      
      - name: Validate OpenAPI spec
        run: |
          redocly lint docs/apis/openapi.yaml
          spectral lint docs/apis/openapi.yaml
      
      - name: Validate API contract
        run: make validate-api-docs
      
      - name: Upload documentation artifacts
        if: always()
        uses: actions/upload-artifact@v3
        with:
          name: api-documentation
          path: |
            docs/apis/openapi.yaml
            .tmp/schemas.yaml
            .tmp/providers.yaml
```

## Developer Guide

### Adding a New Endpoint

1. **Implement the handler**:
```go
// internal/services/api/new_handler.go
type NewFeatureRequest struct {
    Name string `json:"name" binding:"required" openapi:"description:Feature name"`
    Type string `json:"type" binding:"required" openapi:"enum:basic,advanced"`
}

type NewFeatureResponse struct {
    ID        string    `json:"id" openapi:"example:feat_123"`
    Name      string    `json:"name"`
    Type      string    `json:"type"`
    CreatedAt time.Time `json:"created_at"`
}

func (h *NewHandlers) CreateFeature(c *gin.Context) {
    var req NewFeatureRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        // Handle error
        return
    }
    
    // Implementation
    
    c.JSON(200, NewFeatureResponse{
        // Response data
    })
}
```

2. **Add route to router**:
```go
// internal/services/api/router.go
{
    Method:  http.MethodPost,
    Path:    "/:tenantID/features",
    Handler: newHandlers.CreateFeature,
    AuthScope: AuthScopeAdminOrTenant,
    Mode: RouteModeAlways,
},
```

3. **Generate documentation**:
```bash
make generate-schemas
```

4. **Validate**:
```bash
make validate-api-docs
```

5. **Commit both code and documentation**:
```bash
git add internal/services/api/new_handler.go
git add internal/services/api/router.go
git add docs/apis/openapi.yaml
git commit -m "feat: Add new feature endpoint with documentation"
```

### Modifying Existing Endpoint

1. Update the handler/structs
2. Run `make generate-schemas`
3. Review generated changes
4. Run `make validate-api-docs`
5. Commit all changes together

### Adding Provider Type

1. Implement provider in `internal/destregistry/providers/`
2. Add metadata definition
3. Run `make generate-schemas`
4. Documentation automatically includes new provider schemas
5. Commit implementation and documentation
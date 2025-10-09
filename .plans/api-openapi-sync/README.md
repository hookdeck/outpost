# API and OpenAPI Documentation Synchronization Plan

## Overview
This document outlines a comprehensive strategy for maintaining synchronization between the Outpost API implementation (`internal/services/api`) and its OpenAPI documentation (`docs/apis/openapi.yaml`).

## Current State Analysis

### API Implementation Structure
- **Location**: `internal/services/api/`
- **Main Router**: `router.go` - Defines all API routes and their handlers
- **Handlers**: 
  - `tenant_handlers.go` - Tenant management endpoints
  - `destination_handlers.go` - Destination CRUD operations
  - `publish_handlers.go` - Event publishing
  - `log_handlers.go` - Event and delivery logs
  - `retry_handlers.go` - Event retry functionality
  - `topic_handlers.go` - Topic management

### Destination Type Registry
- **Location**: `internal/destregistry/`
- **Provider Registration**: `providers/register.go`
- **Metadata Management**: `metadata/` directory
- **Dynamic Provider Loading**: Provider types are registered at runtime

### OpenAPI Documentation
- **Location**: `docs/apis/openapi.yaml`
- **Current Version**: 0.0.1
- **Structure**: Standard OpenAPI 3.1.0 specification

## Key Challenges

1. **Dynamic Destination Types**: Destination providers are registered dynamically, making it difficult to maintain static OpenAPI schemas
2. **Request/Response Structs**: Go structs in handlers are not directly linked to OpenAPI schemas
3. **Authentication Modes**: Multiple auth modes (API Key, JWT) with different route availability
4. **Manual Updates**: No automated process to detect API changes and update documentation

## Synchronization Strategy

### 1. Automated Generation Approach

#### 1.1 OpenAPI Generation from Code
Create a tool that generates OpenAPI documentation from the Go code:

```go
// cmd/openapi-gen/main.go
package main

import (
    "github.com/hookdeck/outpost/internal/services/api"
    "github.com/hookdeck/outpost/internal/destregistry"
    // OpenAPI generation library
)

func main() {
    // 1. Initialize router to extract routes
    // 2. Inspect handler structs for request/response schemas
    // 3. Load destination metadata for provider schemas
    // 4. Generate OpenAPI spec
}
```

#### 1.2 Implementation Steps
1. Use reflection to analyze handler structs
2. Extract JSON tags for field names and types
3. Parse route definitions from router.go
4. Generate path operations with appropriate auth requirements
5. Include destination provider metadata as schemas

### 2. Code Generation from OpenAPI (Alternative)

#### 2.1 OpenAPI-First Development
Maintain OpenAPI as the source of truth and generate:
- Request/response struct definitions
- Route registration code
- Validation middleware

#### 2.2 Tools
- Use `oapi-codegen` or similar tools
- Custom templates for Gin framework integration
- Automated validation against implementation

### 3. Hybrid Approach (Recommended)

#### 3.1 Core Components
1. **Manual OpenAPI Maintenance**: Core API structure maintained manually
2. **Automated Schema Generation**: Request/response schemas generated from Go structs
3. **Dynamic Provider Documentation**: Auto-generate destination provider schemas from metadata
4. **Validation Pipeline**: CI/CD checks for consistency

#### 3.2 Implementation Plan

##### Phase 1: Schema Generation Tool
```bash
# Generate schemas from Go structs
go run cmd/schema-gen/main.go > schemas.yaml

# Merge with base OpenAPI document
yq merge docs/apis/openapi.yaml schemas.yaml > openapi-merged.yaml
```

##### Phase 2: Provider Documentation Generator
```bash
# Generate provider schemas from metadata
go run cmd/provider-doc-gen/main.go > providers.yaml

# Include in OpenAPI documentation
```

##### Phase 3: Validation Pipeline
```yaml
# .github/workflows/api-docs-check.yml
name: API Documentation Sync Check
on: [push, pull_request]
jobs:
  check-sync:
    steps:
      - name: Generate current API spec
        run: make generate-openapi
      - name: Compare with committed spec
        run: diff docs/apis/openapi.yaml generated-openapi.yaml
```

## Implementation Guidelines

### 1. Struct Annotations
Enhance Go structs with OpenAPI metadata:

```go
type CreateDestinationRequest struct {
    // openapi:required
    Type string `json:"type" binding:"required" openapi:"description:Destination provider type"`
    
    // openapi:required
    Topics models.Topics `json:"topics" binding:"required" openapi:"description:List of topics to subscribe"`
    
    Config      models.Config      `json:"config" openapi:"description:Provider-specific configuration"`
    Credentials models.Credentials `json:"credentials" openapi:"description:Authentication credentials"`
}
```

### 2. Route Documentation
Add documentation comments to route definitions:

```go
// CreateDestination creates a new destination
// @Summary Create destination
// @Description Create a new destination for a tenant
// @Tags Destinations
// @Accept json
// @Produce json
// @Param tenantID path string true "Tenant ID"
// @Param body body CreateDestinationRequest true "Destination configuration"
// @Success 201 {object} DestinationResponse
// @Router /tenants/{tenantID}/destinations [post]
func (h *DestinationHandlers) Create(c *gin.Context) {
    // Implementation
}
```

### 3. Provider Metadata Integration
Automatically include provider schemas in OpenAPI:

```go
func GenerateProviderSchemas() map[string]interface{} {
    schemas := make(map[string]interface{})
    
    for _, provider := range registry.ListProviders() {
        metadata := provider.Metadata()
        
        // Generate config schema
        configSchema := generateSchemaFromFields(metadata.ConfigFields)
        schemas[provider.Type + "Config"] = configSchema
        
        // Generate credentials schema
        credSchema := generateSchemaFromFields(metadata.CredentialFields)
        schemas[provider.Type + "Credentials"] = credSchema
    }
    
    return schemas
}
```

## Validation and Testing

### 1. Contract Testing
Implement contract tests to verify API matches documentation:

```go
func TestAPIContract(t *testing.T) {
    // Load OpenAPI spec
    spec := loadOpenAPISpec()
    
    // Test each endpoint
    for path, operations := range spec.Paths {
        for method, operation := range operations {
            t.Run(fmt.Sprintf("%s %s", method, path), func(t *testing.T) {
                // Make request
                // Validate response matches schema
            })
        }
    }
}
```

### 2. Documentation Linting
Use OpenAPI linters in CI:

```yaml
- name: Lint OpenAPI
  run: |
    npx @redocly/cli lint docs/apis/openapi.yaml
    npx @stoplight/spectral-cli lint docs/apis/openapi.yaml
```

## Maintenance Process

### 1. Development Workflow
1. **API Changes**: Update handler code and structs
2. **Documentation**: Run schema generation
3. **Validation**: Execute contract tests
4. **Review**: Manual review of generated changes
5. **Commit**: Include both code and documentation updates

### 2. Review Checklist
- [ ] Handler methods match OpenAPI operations
- [ ] Request/response schemas are accurate
- [ ] Authentication requirements are correct
- [ ] Error responses are documented
- [ ] Examples are up-to-date

### 3. Regular Audits
Monthly tasks:
- Run full API-to-documentation comparison
- Update provider schemas from metadata
- Review and update examples
- Check for undocumented endpoints

## Tooling Requirements

### 1. Required Tools
- **swaggo/swag**: Generate OpenAPI from Go annotations
- **deepmap/oapi-codegen**: Generate Go code from OpenAPI
- **yq**: YAML manipulation
- **spectral**: OpenAPI linting

### 2. Custom Tools to Develop
- `schema-gen`: Extract schemas from Go structs
- `provider-doc-gen`: Generate provider documentation
- `api-diff`: Compare API implementation with documentation
- `contract-test-gen`: Generate tests from OpenAPI

## Migration Path

### Phase 1: Foundation (Week 1-2)
1. Set up schema generation tool
2. Add annotations to existing structs
3. Generate initial schemas

### Phase 2: Automation (Week 3-4)
1. Implement CI/CD validation
2. Create contract tests
3. Set up linting

### Phase 3: Provider Integration (Week 5-6)
1. Generate provider schemas
2. Integrate with main OpenAPI doc
3. Validate all endpoints

### Phase 4: Maintenance (Ongoing)
1. Document process for developers
2. Regular audits
3. Continuous improvement

## Success Metrics

1. **Coverage**: 100% of API endpoints documented
2. **Accuracy**: Zero discrepancies in contract tests
3. **Automation**: < 5 minutes to validate sync
4. **Developer Experience**: Clear process and tooling

## Next Steps

1. **Immediate Actions**:
   - Create `cmd/schema-gen` tool
   - Add OpenAPI annotations to one handler as POC
   - Set up basic CI validation

2. **Short Term** (1 month):
   - Complete schema generation for all handlers
   - Implement provider documentation generation
   - Add contract testing

3. **Long Term** (3 months):
   - Full automation pipeline
   - Developer documentation
   - Regular audit process

## References

- [OpenAPI 3.1.0 Specification](https://spec.openapis.org/oas/v3.1.0)
- [swaggo Documentation](https://github.com/swaggo/swag)
- [oapi-codegen](https://github.com/deepmap/oapi-codegen)
- [Spectral Linting](https://github.com/stoplightio/spectral)
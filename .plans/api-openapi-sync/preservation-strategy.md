# OpenAPI Spec Preservation Strategy

## 🎯 Answer: The Plan PRESERVES Your Existing OpenAPI Spec

The recommended approach **does NOT completely rewrite** your existing OpenAPI specification. Instead, it uses a **split-and-merge strategy** that preserves your manually-maintained content while automating the repetitive parts.

## How It Works

### 1. Split the Current OpenAPI Into Two Parts

#### `docs/apis/openapi-base.yaml` (Manually Maintained)
Preserves all your custom, carefully crafted content:
```yaml
openapi: 3.1.0
info:
  title: Outpost API
  version: "0.0.1"
  description: The Outpost API is a REST-based JSON API...
  contact:
    name: Outpost Support
    email: support@hookdeck.com
    url: https://outpost.hookdeck.com/docs

servers:
  - url: http://localhost:3333/api/v1
    description: Local development server base path

security:
  - AdminApiKey: []
  - TenantJwt: []

# Paths section with descriptions, examples, custom documentation
paths:
  /api/v1/publish:
    post:
      summary: Publish an event
      description: Your custom endpoint descriptions remain here
      # ... operation details ...

# Custom components that aren't auto-generated
components:
  securitySchemes:
    AdminApiKey:
      type: http
      scheme: bearer
      description: Admin API Key configured via API_KEY...
    # ... other security schemes ...
```

#### Auto-Generated Schemas (Generated from Code)
Only the request/response schemas and provider configurations are auto-generated:
```yaml
components:
  schemas:
    # These are generated from Go structs
    CreateDestinationRequest:
      type: object
      properties: # Generated from struct
    UpdateDestinationRequest:
      type: object
      properties: # Generated from struct
    
    # Provider schemas generated from metadata
    WebhookConfig:
      type: object
      properties: # Generated from provider metadata
    AWSSQSConfig:
      type: object
      properties: # Generated from provider metadata
```

### 2. Merge Strategy

The build process merges these files:
```bash
# Step 1: Generate schemas from Go code
go run cmd/schema-gen/main.go > .tmp/schemas.yaml

# Step 2: Generate provider schemas
go run cmd/provider-doc-gen/main.go > .tmp/providers.yaml

# Step 3: Merge with your base (preserving your content)
yq eval-all 'select(fileIndex == 0) * select(fileIndex == 1)' \
    docs/apis/openapi-base.yaml .tmp/schemas.yaml > .tmp/merged.yaml

# Step 4: Add provider schemas
yq eval-all 'select(fileIndex == 0) * select(fileIndex == 1)' \
    .tmp/merged.yaml .tmp/providers.yaml > docs/apis/openapi.yaml
```

## What Gets Preserved vs Generated

### ✅ PRESERVED (Your Manual Work Stays)
- API information (title, version, description, contact)
- Server configurations
- Security definitions
- Path definitions and operations
- Custom endpoint descriptions
- Parameter descriptions
- Response descriptions
- Example values in paths
- Custom error responses
- Rate limiting documentation
- Any custom x-extensions
- Webhook configurations
- Business logic documentation

### 🔄 AUTO-GENERATED (From Code)
- Request/response schemas (from Go structs)
- Model definitions (Tenant, Destination, Event, etc.)
- Provider configuration schemas (from metadata)
- Provider credential schemas (from metadata)
- Field types and validations (from struct tags)
- Required field markers (from binding tags)

## Migration Process

### Step 1: Initial Split (One-Time)
```bash
# Backup current spec
cp docs/apis/openapi.yaml docs/apis/openapi.yaml.backup

# Create base file with non-generated content
cp docs/apis/openapi.yaml docs/apis/openapi-base.yaml

# Edit openapi-base.yaml to remove schemas that will be auto-generated
# Remove from components.schemas:
# - CreateDestinationRequest
# - UpdateDestinationRequest  
# - DestinationWebhook
# - DestinationAWSSQS
# - (other auto-generatable schemas)

# Keep in openapi-base.yaml:
# - Paths
# - Info
# - Servers
# - Security
# - Custom schemas like PaginatedResponse, Topics, SuccessResponse
```

### Step 2: First Generation
```bash
# Generate new schemas
make generate-schemas

# Compare with backup
diff docs/apis/openapi.yaml.backup docs/apis/openapi.yaml

# Verify nothing important was lost
```

### Step 3: Ongoing Maintenance
```bash
# When you change API code:
make generate-schemas

# When you add custom documentation:
# Edit docs/apis/openapi-base.yaml

# The final openapi.yaml is always regenerated
```

## Example: Adding a New Endpoint

### Your Work (Manual)
Edit `docs/apis/openapi-base.yaml`:
```yaml
paths:
  /api/v1/{tenantID}/features:
    post:
      summary: Create a new feature
      description: |
        Creates a new feature for the tenant.
        This endpoint requires authentication.
      tags: [Features]
      parameters:
        - name: tenantID
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateFeatureRequest'
      responses:
        '201':
          description: Feature created successfully
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/FeatureResponse'
```

### Auto-Generated Part
The schemas are generated from your Go code:
```go
type CreateFeatureRequest struct {
    Name string `json:"name" binding:"required"`
    Type string `json:"type" binding:"required"`
}

type FeatureResponse struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Type      string    `json:"type"`
    CreatedAt time.Time `json:"created_at"`
}
```

## Benefits of This Approach

1. **No Loss of Work**: All your custom documentation is preserved
2. **Reduced Duplication**: Schemas come from single source of truth (Go code)
3. **Type Safety**: Changes to structs automatically update documentation
4. **Flexibility**: You control what's manual vs automated
5. **Gradual Migration**: Can adopt incrementally

## FAQ

### Q: What happens to my custom schema descriptions?
**A:** They can be preserved in the base file or moved to Go struct tags:
```go
type Destination struct {
    ID string `json:"id" openapi:"description:Unique identifier for the destination"`
}
```

### Q: Can I override auto-generated schemas?
**A:** Yes, by defining them in openapi-base.yaml instead. The merge process respects manual definitions.

### Q: What about breaking changes?
**A:** The CI/CD pipeline will detect if generated schemas change in breaking ways, allowing review before merge.

### Q: How do I document things not in the code?
**A:** Keep them in openapi-base.yaml. Examples:
- Rate limiting headers
- Webhook payloads from external services  
- Future/deprecated endpoints
- API concepts and guides

## Summary

The approach **enhances** your existing OpenAPI spec rather than replacing it. You maintain full control over the important documentation while automating the tedious, error-prone task of keeping schemas synchronized with code.

Your investment in the current OpenAPI documentation is preserved and enhanced, not discarded.
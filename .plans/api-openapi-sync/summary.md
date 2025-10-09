# API-OpenAPI Synchronization Plan Summary

## 📋 Overview
This plan addresses the challenge of keeping the Outpost API implementation (`internal/services/api/`) synchronized with its OpenAPI documentation (`docs/apis/openapi.yaml`).

## 🎯 Problem Statement
Currently, there is no consistent way to ensure the API implementation and documentation stay in sync. Changes to the API require manual updates to the OpenAPI specification, leading to:
- Documentation drift
- Inconsistent schemas
- Missing endpoint documentation
- Outdated examples
- Dynamic provider types not reflected in docs

## ✅ Solution: Hybrid Approach

### Core Strategy
1. **Automated Schema Generation**: Extract schemas from Go structs using reflection
2. **Provider Documentation**: Generate provider schemas from metadata registry
3. **CI/CD Validation**: Automated checks to ensure synchronization
4. **Contract Testing**: Verify API behavior matches documentation

### Key Components

#### 1. Schema Generator (`cmd/schema-gen`)
- Analyzes Go structs in handlers
- Extracts JSON tags and validation rules
- Generates OpenAPI schemas
- Supports custom OpenAPI annotations

#### 2. Provider Documentation Generator (`cmd/provider-doc-gen`)
- Reads provider metadata from registry
- Generates config and credential schemas
- Handles dynamic provider types
- Marks sensitive fields

#### 3. Validation Pipeline
- GitHub Actions workflow
- Compares generated vs committed schemas
- Runs contract tests
- Lints OpenAPI specification

## 📁 Deliverables

### Documentation Files Created
1. **README.md** - Complete strategy and implementation guide
2. **implementation-example.md** - Working code examples
3. **action-plan.md** - Step-by-step implementation timeline
4. **summary.md** - This executive summary

### Tools to Develop
- `cmd/schema-gen` - Generate schemas from Go structs
- `cmd/provider-doc-gen` - Generate provider documentation
- `cmd/validate-api-docs` - Validate API against OpenAPI
- Makefile targets for automation
- GitHub Actions workflow

## 🚀 Implementation Timeline

### Week 1: Foundation
- Basic schema generator
- Provider schema generation
- Initial struct annotations

### Week 2-3: Automation
- CI/CD pipeline setup
- Contract testing
- Developer documentation

### Month 2: Enhancement
- Advanced features
- Error schemas
- Documentation preview

### Month 3+: Full Automation
- Bi-directional sync
- Version management
- Documentation portal

## 💡 Key Benefits

1. **Consistency**: API and documentation always in sync
2. **Automation**: Minimal manual intervention required
3. **Type Safety**: Leverages Go's type system
4. **Dynamic Support**: Handles runtime-registered providers
5. **Developer Experience**: Clear process and tooling
6. **Quality Assurance**: Automated validation and testing

## 🔧 Technical Approach

### For Existing Code
```go
// Add OpenAPI annotations to structs
type CreateDestinationRequest struct {
    Type string `json:"type" binding:"required" openapi:"description:Provider type"`
    Topics []string `json:"topics" binding:"required" openapi:"description:Event topics"`
}
```

### For New Endpoints
1. Implement handler with annotated structs
2. Add route to router
3. Run `make generate-schemas`
4. Validate with `make validate-api-docs`
5. Commit code and documentation together

### For Provider Types
- Providers automatically documented from metadata
- No manual documentation needed
- Schemas generated at build time

## 📊 Success Metrics

- **Coverage**: 100% of endpoints documented
- **Accuracy**: Zero contract test failures
- **Speed**: < 5 minutes to validate
- **Adoption**: All developers using process

## 🎬 Next Actions

### Immediate (This Week)
1. Create `cmd/schema-gen` tool
2. Add OpenAPI tags to 5 structs
3. Set up basic validation

### Short Term (Month 1)
1. Complete all struct annotations
2. Implement CI/CD pipeline
3. Add contract testing

### Long Term (3 Months)
1. Full automation
2. Documentation portal
3. SDK generation

## 🛠️ Quick Start

```bash
# Generate schemas
make generate-schemas

# Validate documentation
make validate-api-docs

# Check if in sync
make check-api-sync

# Preview documentation
docker run -p 8080:8080 -v $(pwd)/docs/apis:/usr/share/nginx/html/swagger-ui \
  -e SWAGGER_JSON=/swagger-ui/openapi.yaml swaggerapi/swagger-ui
```

## 📚 Resources

- **Documentation**: `.plans/api-openapi-sync/`
- **OpenAPI Spec**: `docs/apis/openapi.yaml`
- **API Implementation**: `internal/services/api/`
- **Provider Registry**: `internal/destregistry/`

## ✨ Conclusion

This plan provides a comprehensive solution for maintaining API-documentation synchronization. The hybrid approach balances automation with flexibility, ensuring that both static API endpoints and dynamic provider types are properly documented. The phased implementation allows for gradual adoption while delivering immediate value.

The key innovation is handling dynamic destination providers by generating their schemas from the metadata registry, ensuring new providers are automatically documented without manual intervention.
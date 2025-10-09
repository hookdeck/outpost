# Action Plan: API-OpenAPI Synchronization Implementation

## Immediate Actions (Week 1)

### Day 1-2: Setup Foundation
1. **Create basic schema generator**
   ```bash
   mkdir -p cmd/schema-gen
   # Implement basic struct-to-schema conversion
   # Test with CreateDestinationRequest and UpdateDestinationRequest
   ```

2. **Split OpenAPI document**
   ```bash
   cp docs/apis/openapi.yaml docs/apis/openapi-base.yaml
   # Remove auto-generatable schemas from base
   # Keep only static content (info, servers, security)
   ```

3. **Update .gitignore**
   ```
   .tmp/
   generated-openapi.yaml
   ```

### Day 3-4: Enhance Structs
1. **Add OpenAPI tags to handler structs**
   - Start with `destination_handlers.go`
   - Add description, example, and validation tags
   - Document all fields

2. **Create validation test**
   ```go
   // internal/services/api/openapi_test.go
   func TestStructsHaveOpenAPITags(t *testing.T) {
       // Verify all exported structs have proper tags
   }
   ```

### Day 5: Provider Integration
1. **Create provider schema generator**
   ```bash
   mkdir -p cmd/provider-doc-gen
   # Generate schemas from metadata
   # Handle all registered providers
   ```

2. **Test with existing providers**
   - webhook
   - aws_sqs
   - rabbitmq
   - hookdeck
   - aws_kinesis
   - azure_servicebus
   - aws_s3

## Short Term (Week 2-3)

### Week 2: Automation
1. **Makefile targets**
   ```makefile
   generate-schemas:
   validate-api-docs:
   check-api-sync:
   ```

2. **CI/CD pipeline**
   - GitHub Actions workflow
   - Pre-commit hooks
   - PR checks

3. **Contract testing**
   - Test each endpoint against OpenAPI
   - Validate request/response schemas
   - Check auth requirements

### Week 3: Documentation
1. **Developer guide**
   - How to add new endpoints
   - How to modify existing APIs
   - How to add provider types

2. **Migration of existing code**
   - Add tags to all structs
   - Generate complete documentation
   - Validate against current implementation

## Medium Term (Month 2)

### Advanced Features
1. **Endpoint documentation from code**
   - Extract route definitions
   - Generate path operations
   - Include auth requirements

2. **Error response schemas**
   - Standard error format
   - Validation error details
   - Provider-specific errors

3. **Example generation**
   - Request examples from tests
   - Response examples from fixtures
   - Provider configuration examples

### Quality Improvements
1. **OpenAPI linting rules**
   - Custom Spectral rules
   - Outpost-specific validations
   - Naming conventions

2. **Documentation preview**
   - Swagger UI integration
   - Redoc integration
   - PR preview deployments

## Long Term (Month 3+)

### Full Automation
1. **Code generation from OpenAPI**
   - Generate request/response structs
   - Generate validation middleware
   - Generate client SDKs

2. **Version management**
   - API versioning strategy
   - Breaking change detection
   - Migration guides

3. **Documentation portal**
   - Interactive API explorer
   - Provider catalog
   - Code examples in multiple languages

## Success Criteria

### Week 1
- [ ] Basic schema generator working
- [ ] Provider schemas generated
- [ ] Manual validation passing

### Week 2
- [ ] Automated generation in CI
- [ ] All handlers have OpenAPI tags
- [ ] Contract tests implemented

### Week 3
- [ ] Documentation complete
- [ ] Developer guide published
- [ ] Team trained on process

### Month 2
- [ ] Full automation pipeline
- [ ] Zero manual steps for common tasks
- [ ] < 5 minute validation time

### Month 3
- [ ] Bi-directional sync capability
- [ ] Version management in place
- [ ] Documentation portal live

## Risk Mitigation

### Technical Risks
1. **Complex type handling**
   - Solution: Start with simple types, gradually add complex ones
   - Fallback: Manual documentation for edge cases

2. **Provider metadata changes**
   - Solution: Version provider schemas
   - Monitor: Alert on schema changes

3. **Performance impact**
   - Solution: Cache generated schemas
   - Optimize: Run generation only on changes

### Process Risks
1. **Developer adoption**
   - Solution: Clear documentation and tooling
   - Training: Workshop for team

2. **Maintenance burden**
   - Solution: Maximum automation
   - Review: Regular process improvements

## Resource Requirements

### Tools
- Go development environment
- YAML processing tools (yq)
- OpenAPI tools (spectral, redocly)
- CI/CD pipeline access

### Time Investment
- Initial setup: 1 week
- Full implementation: 1 month
- Maintenance: 2-4 hours/week

### Team Involvement
- API developers: Add tags to structs
- DevOps: Set up CI/CD pipeline
- Documentation team: Review generated docs

## Next Steps Checklist

### Immediate (Do Today)
- [ ] Create `.plans/api-openapi-sync` directory structure
- [ ] Set up cmd/schema-gen skeleton
- [ ] Identify first struct to convert

### This Week
- [ ] Implement basic schema generator
- [ ] Add OpenAPI tags to 5 handler structs
- [ ] Create first validation test

### Next Week
- [ ] Set up CI/CD pipeline
- [ ] Complete provider schema generation
- [ ] Document the process

## Commands Quick Reference

```bash
# Generate schemas
make generate-schemas

# Validate documentation
make validate-api-docs

# Check synchronization
make check-api-sync

# Run contract tests
go test ./internal/services/api/... -tags=contract

# Preview documentation
docker run -p 8080:8080 -v $(pwd)/docs/apis:/usr/share/nginx/html/swagger-ui \
  -e SWAGGER_JSON=/swagger-ui/openapi.yaml swaggerapi/swagger-ui

# Lint OpenAPI
npx @redocly/cli lint docs/apis/openapi.yaml
npx @stoplight/spectral-cli lint docs/apis/openapi.yaml
```

## Contact & Support

- **Lead**: API Team
- **Slack Channel**: #api-documentation
- **Repository**: `.plans/api-openapi-sync/`
- **Issues**: GitHub Issues with `api-docs` label
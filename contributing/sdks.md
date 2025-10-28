# Contributing to Outpost SDKs

This guide covers the complete process for updating, generating, testing, and publishing Outpost SDKs.

## Overview

Outpost SDKs are automatically generated from the [OpenAPI specification](../docs/apis/openapi.yaml) using [Speakeasy](https://www.speakeasyapi.dev/). The SDK generation, testing, and publishing process is managed through GitHub Actions workflows, but requires manual triggering and careful coordination to ensure quality.

**Key Point**: SDK generation is a **manual process** that requires explicit workflow triggering and version management. This is intentional to maintain control over releases.

## Prerequisites

### Required Tools
- Git
- GitHub account with write access to the Outpost repository
- For local testing:
  - **Go SDK**: Go 1.21+
  - **Python SDK**: Python 3.9+, pip
  - **TypeScript SDK**: Node.js 18+, npm

### Required Access & Secrets
The following secrets must be configured in the repository (for maintainers):
- `SPEAKEASY_API_KEY`: For SDK generation
- `NPM_TOKEN`: For publishing TypeScript SDK to npm
- `PYPI_API_TOKEN`: For publishing Python SDK to PyPI
- GitHub token (automatic): For publishing Go SDK

## SDK Architecture

Outpost maintains three official SDKs, each in its own directory within the `sdks/` folder:

### 1. Go SDK (`sdks/outpost-go/`)
- **Location**: `sdks/outpost-go/`
- **Package**: `github.com/hookdeck/outpost-go`
- **Published**: Automatically via Git tags
- **Generation Workflow**: `sdk_generation_outpost_go.yaml`
- **Publish Workflow**: `sdk_publish_outpost_go.yaml`

### 2. Python SDK (`sdks/outpost-python/`)
- **Location**: `sdks/outpost-python/`
- **Package**: `outpost-python` on PyPI
- **Published**: Automatically to PyPI on merge
- **Generation Workflow**: `sdk_generation_outpost_python.yaml`
- **Publish Workflow**: `sdk_publish_outpost_python.yaml`

### 3. TypeScript SDK (`sdks/outpost-typescript/`)
- **Location**: `sdks/outpost-typescript/`
- **Package**: `@hookdeck/outpost` on npm (also `@hookdeck/outpost-sdk`)
- **Published**: Automatically to npm on merge
- **Generation Workflow**: `sdk_generation_outpost_ts.yaml`
- **Publish Workflow**: `sdk_publish_outpost_ts.yaml`

Each SDK is maintained as a sub-project within this repository and generated from the shared [OpenAPI specification](../docs/apis/openapi.yaml).

## Updating the OpenAPI Specification

All SDK changes begin with updating the OpenAPI specification:

1. **Edit the specification**:
   ```bash
   # The OpenAPI spec is located at:
   docs/apis/openapi.yaml
   ```

2. **Make your changes**:
   - Add new endpoints, modify existing ones, or update schemas
   - Follow OpenAPI 3.0 standards
   - Ensure all changes are backward compatible when possible
   - Add descriptions and examples for better SDK documentation

3. **Validate the specification**:
   ```bash
   # Use Speakeasy CLI to validate (if installed)
   speakeasy validate -s docs/apis/openapi.yaml
   
   # Or use the spec-sdk-tests validation
   cd spec-sdk-tests
   npm run validate:spec
   ```

4. **Commit and push**:
   ```bash
   git add docs/apis/openapi.yaml
   git commit -m "Update OpenAPI spec: <description>"
   git push
   ```

## SDK Generation Workflow

The complete workflow from OpenAPI spec update to published SDKs:

### Step 1: Update OpenAPI Specification
See [Updating the OpenAPI Specification](#updating-the-openapi-specification) above.

### Step 2: Manually Trigger SDK Generation

SDK generation is **manually triggered** through GitHub Actions:

1. **Navigate to Actions**:
   - Go to the [Actions tab](https://github.com/hookdeck/outpost/actions) in GitHub

2. **Select the workflow**:
   - `Generate OUTPOST-GO`
   - `Generate OUTPOST-PYTHON`
   - `Generate OUTPOST-TS`

3. **Run the workflow**:
   - Click "Run workflow"
   - Select the branch (typically `main`)
   - **Optional inputs**:
     - `force`: Force generation even if no OpenAPI changes detected (default: false)
     - `set_version`: Set a specific SDK version (e.g., `1.2.3`). Leave empty to keep current version
   - Click "Run workflow"

4. **Repeat for all three SDKs** that need updates

**Version Bumping**: The `set_version` input allows you to manually specify the SDK version. If left empty, the SDK will regenerate with the current version. Follow [semantic versioning](#versioning-guidelines) when setting versions.

### Step 3: Review Generated SDK Pull Requests

Each workflow creates a pull request in the respective SDK repository:

1. **Wait for workflows to complete** (~5-10 minutes per SDK)

2. **Review the PRs**:
   - **Go**: Check `sdks/outpost-go` for new PR
   - **Python**: Check `sdks/outpost-python` for new PR
   - **TypeScript**: Check `sdks/outpost-typescript` for new PR

3. **Verify changes**:
   - Review the diff to ensure changes match your OpenAPI updates
   - Check for any unexpected modifications
   - Ensure documentation comments are properly generated
   - Verify examples compile/run
   - Review the `.speakeasy/gen.lock` file changes

### Step 4: Test the Generated SDKs

> ⚠️ **CRITICAL**: Before testing, you **MUST** regenerate the TypeScript SDK. See [Testing with spec-sdk-tests](#testing-with-spec-sdk-tests) below.

Testing is **mandatory** before merging:

1. **Test with spec-sdk-tests** (required for TypeScript):
   - See the dedicated [Testing with spec-sdk-tests](#testing-with-spec-sdk-tests) section below
   - This step catches integration issues early

2. **Run SDK-specific tests**:
   ```bash
   # In each SDK directory
   cd sdks/outpost-go && go test ./...
   cd sdks/outpost-python && pytest
   cd sdks/outpost-typescript && npm test
   ```

3. **Manual integration testing** (recommended):
   - Test key functionality with a running Outpost instance
   - Verify authentication works
   - Test new/modified endpoints
   - Check error handling

### Step 5: Merge to Trigger Automatic Publishing

Once testing is complete and PRs are approved:

1. **Merge the PR** in each SDK repository
2. **Automatic publishing triggers**:
   - **Go**: New Git tag is created automatically
   - **Python**: Published to PyPI automatically
   - **TypeScript**: Published to npm automatically

3. **Verify publication**:
   - Check the Actions tab for successful publish workflows
   - Verify packages are available:
     - Go: `go get github.com/hookdeck/outpost-go@<version>`
     - Python: Check [PyPI](https://pypi.org/project/outpost-python/)
     - TypeScript: Check [npm](https://www.npmjs.com/package/@hookdeck/outpost)

## Versioning Guidelines

Outpost SDKs follow [Semantic Versioning](https://semver.org/) (SemVer):

### BETA Versioning (Current)

> **Note**: During the BETA phase (0.x.x versions), we bump the **minor version** for all changes, including breaking changes. This allows for rapid iteration and API evolution without committing to long-term stability.

- **Minor version (0.x.0)**: All changes during BETA
  - Breaking changes (normally would be major version)
  - New features and endpoints
  - Bug fixes and improvements
  - API modifications

### Post-BETA Versioning (1.0.0+)

Once Outpost reaches stable 1.0.0, we'll follow strict SemVer:

- **Major version (x.0.0)**: Breaking changes to the API
  - Remove endpoints or parameters
  - Change required fields
  - Modify response structures in incompatible ways

- **Minor version (0.x.0)**: Backward-compatible additions
  - Add new endpoints
  - Add optional parameters
  - Add new response fields

- **Patch version (0.0.x)**: Backward-compatible fixes
  - Fix bugs in SDK code
  - Update documentation
  - Improve error messages

### Version Synchronization

**Important**: SDK versions should generally stay in sync with each other, but can diverge if language-specific fixes are needed. When updating the OpenAPI spec that affects all SDKs, bump all SDK versions consistently.

## Testing with spec-sdk-tests

The `spec-sdk-tests/` directory contains integration tests that verify the TypeScript SDK works correctly with a running Outpost instance.

### ⚠️ CRITICAL: The Race Condition Issue

> **⚠️ WARNING: LOCAL FILE DEPENDENCY RACE CONDITION**
>
> The `spec-sdk-tests` project has a **local file dependency** on the TypeScript SDK, which creates a critical testing race condition:
>
> ```json
> "dependencies": {
>   "@hookdeck/outpost-sdk": "file:../../../sdks/outpost-typescript"
> }
> ```
>
> **The Problem**:
> - The test suite uses the TypeScript SDK **from your local file system**
> - If you regenerate the SDK and test immediately, npm may use **cached/old build artifacts**
> - This means **you might be testing the old SDK version**, not the new one
> - Tests can pass with stale code, hiding actual issues
>
> **Why This Matters**:
> - ❌ False confidence: Tests pass but the new SDK has bugs
> - ❌ Wasted time: Debugging issues that don't exist in the version you're testing
> - ❌ Publishing broken SDKs: The actual published SDK differs from what you tested
> - ❌ Downstream breakage: Users get a broken SDK that you never actually tested

### Step-by-Step Testing Workflow

To avoid the race condition, **always follow this exact sequence**:

1. **Generate/update the TypeScript SDK** (via GitHub Actions or locally)

2. **Rebuild the TypeScript SDK**:
   ```bash
   cd sdks/outpost-typescript
   npm install
   npm run build
   cd ../..
   ```

3. **Clean and reinstall test dependencies**:
   ```bash
   cd spec-sdk-tests
   rm -rf node_modules package-lock.json
   npm install
   ```

4. **Start Outpost locally** (in a separate terminal):
   ```bash
   # Using Docker Compose
   docker-compose -f build/dev/compose.yml up
   
   # Or run directly with Go
   go run cmd/outpost/main.go
   
   # Verify it's running
   curl http://localhost:8000/healthz
   ```

5. **Run the tests**:
   ```bash
   npm test
   ```

6. **Verify test output**:
   - All tests should pass
   - Check for any warnings or deprecation notices
   - Verify the SDK version being tested matches your expectations
   - Review any console output for unexpected behavior

### What These Tests Cover

The `spec-sdk-tests` verify:
- SDK can authenticate with Outpost
- API endpoints return expected responses
- Request/response serialization works correctly
- Error handling behaves as expected
- New functionality from OpenAPI changes works
- Schema validation and type safety
- Integration with real Outpost instance

### When to Run These Tests

**Always run these tests**:
- ✅ After generating a new TypeScript SDK
- ✅ Before merging SDK generation PRs
- ✅ After making breaking changes to the OpenAPI spec
- ✅ When debugging SDK issues reported by users

**Optional but recommended**:
- After updating dependencies in the TypeScript SDK
- When testing integration with new Outpost features
- Before major releases

## Publishing Process

Publishing is **fully automated** after merging:

### Go SDK Publishing
- **Trigger**: Merge to `main` branch in `sdks/outpost-go`
- **Workflow**: `.github/workflows/sdk_publish_outpost_go.yaml`
- **Process**:
  1. Runs tests
  2. Creates a Git tag with the version from `.speakeasy/gen.lock`
  3. Pushes tag to repository
  4. Go users can `go get` the new version
- **Verification**: Check [GitHub releases](https://github.com/hookdeck/outpost-go/releases)

### Python SDK Publishing
- **Trigger**: Merge to `main` branch in `sdks/outpost-python`
- **Workflow**: `.github/workflows/sdk_publish_outpost_python.yaml`
- **Process**:
  1. Runs tests
  2. Builds distribution packages
  3. Publishes to PyPI using `PYPI_API_TOKEN`
  4. Available via `pip install outpost-python`
- **Verification**: Check [PyPI](https://pypi.org/project/outpost-python/)

### TypeScript SDK Publishing
- **Trigger**: Merge to `main` branch in `sdks/outpost-typescript`
- **Workflow**: `.github/workflows/sdk_publish_outpost_ts.yaml`
- **Process**:
  1. Runs tests
  2. Builds the package
  3. Publishes to npm using `NPM_TOKEN`
  4. Available via `npm install @hookdeck/outpost`
- **Verification**: Check [npm](https://www.npmjs.com/package/@hookdeck/outpost)

### Monitoring Publication

After merging, monitor the publish workflows:

1. Go to the **Actions tab** in the Outpost repository
2. Watch the publish workflow execution for each SDK
3. Verify successful completion
4. Check package registries for the new version

If publication fails:
- Check the workflow logs for errors
- Verify secrets are correctly configured
- Ensure version numbers don't conflict with existing releases
- Look for path trigger issues (`.speakeasy/gen.lock` changes)

## Building and Testing SDKs Locally (Optional)

> **Note**: This section is for advanced use cases. SDK building and testing is automatically handled in CI/CD when PRs are created. You typically only need this for debugging or local development of the SDKs themselves.

While the CI/CD pipeline handles SDK generation, you can build and test locally if needed:

### Go SDK
```bash
cd sdks/outpost-go
go mod download
go build ./...
go test ./...
```

### Python SDK
```bash
cd sdks/outpost-python
pip install -e .
pip install pytest
pytest
```

### TypeScript SDK
```bash
cd sdks/outpost-typescript
npm install
npm run build
npm test
```

## Troubleshooting

### SDK Generation Fails

**Symptom**: Workflow fails during generation step

**Solutions**:
1. Validate OpenAPI spec: `speakeasy validate -s docs/apis/openapi.yaml`
2. Check for syntax errors in `openapi.yaml`
3. Ensure all `$ref` references are valid
4. Review workflow logs for specific Speakeasy errors
5. Verify `SPEAKEASY_API_KEY` secret is configured
6. Check if the target exists in `.speakeasy/workflow.yaml`

### Tests Fail in Generated SDK

**Symptom**: PR is created but tests fail

**Solutions**:
1. Review the test output in the workflow logs
2. Check if OpenAPI changes introduced breaking changes
3. Verify example code in OpenAPI spec is valid
4. May need to update test fixtures in SDK repositories
5. Ensure dependencies are correctly specified in generated code

### spec-sdk-tests Fail After Regeneration

**Symptom**: Tests fail or show unexpected behavior

**Solutions**:
1. **Did you rebuild the TypeScript SDK?** (Most common issue)
   ```bash
   cd sdks/outpost-typescript && npm run build
   ```
2. **Did you clean test dependencies?**
   ```bash
   cd spec-sdk-tests && rm -rf node_modules && npm install
   ```
3. Is Outpost running locally? Check `http://localhost:3333/healthz`
4. Check for console warnings about version mismatches
5. Review test logs for API errors or authentication issues
6. Verify the file path in `package.json` is correct: `file:../../../sdks/outpost-typescript`
7. Check if the TypeScript SDK build output exists in `sdks/outpost-typescript/dist`

### Publishing Fails

**Symptom**: Merge succeeded but package not published

**Solutions**:
1. Check if publish workflow ran (Actions tab)
2. Verify the publish trigger path: `.speakeasy/gen.lock` should have changed
3. Check secrets are configured: `SPEAKEASY_API_KEY`, `NPM_TOKEN`, `PYPI_API_TOKEN`
4. Look for version conflicts (version already exists)
5. Review publish workflow logs for specific errors
6. For Go: Ensure Git tag was created successfully
7. For npm/PyPI: Check authentication errors in logs

### Version Mismatch

**Symptom**: Testing shows old SDK version or changes not reflected

**Solutions**:
1. **Rebuild the TypeScript SDK** if testing with spec-sdk-tests
2. Verify the PR actually contains your changes (review diff)
3. Check that you're testing against the correct branch
4. Clear npm/pip caches if testing locally:
   ```bash
   npm cache clean --force
   pip cache purge
   ```
5. Confirm `set_version` was provided correctly in workflow trigger
6. Check `.speakeasy/gen.lock` for the actual version being used

### Local File Dependency Issues

**Symptom**: spec-sdk-tests shows import errors or missing types

**Solutions**:
1. Ensure TypeScript SDK is built: `cd sdks/outpost-typescript && npm run build`
2. Check build output exists: `ls sdks/outpost-typescript/dist`
3. Reinstall test dependencies: `cd spec-sdk-tests && npm install`
4. Check for TypeScript compilation errors in the SDK
5. Verify `package.json` dependency path is correct: `"file:../../../sdks/outpost-typescript"`
6. Ensure SDK `package.json` has correct exports/main fields

### Workflow Doesn't Trigger

**Symptom**: Publish workflow doesn't run after merge

**Solutions**:
1. Check the workflow trigger paths in `.github/workflows/sdk_publish_*.yaml`
2. Verify `.speakeasy/gen.lock` file was actually modified in the commit
3. Check workflow permissions (needs write access to contents, PRs, etc.)
4. Look for branch protection rules that might block workflow runs
5. Manually trigger with `workflow_dispatch` if needed

---

## Quick Reference

### Complete SDK Update Checklist

- [ ] Update `docs/apis/openapi.yaml`
- [ ] Validate OpenAPI spec
- [ ] Commit and push changes
- [ ] Manually trigger SDK generation workflows (all 3 SDKs)
- [ ] Set version numbers via `set_version` input (if bumping)
- [ ] Wait for PR creation in each SDK repo (~5-10 min each)
- [ ] Review PRs for correctness
- [ ] **Rebuild TypeScript SDK**: `cd sdks/outpost-typescript && npm run build`
- [ ] **Clean test deps**: `cd spec-sdk-tests && rm -rf node_modules && npm install`
- [ ] Start Outpost locally
- [ ] Run spec-sdk-tests: `cd spec-sdk-tests && npm test`
- [ ] Run SDK-specific tests in each SDK repo
- [ ] Perform manual integration testing (optional but recommended)
- [ ] Approve and merge PRs
- [ ] Verify automatic publishing succeeds
- [ ] Test published packages from registries

### Common Commands

```bash
# Validate OpenAPI spec
speakeasy validate -s docs/apis/openapi.yaml
cd spec-sdk-tests && npm run validate:spec

# Rebuild TypeScript SDK (CRITICAL before testing!)
cd sdks/outpost-typescript
npm install
npm run build
cd ../..

# Regenerate SDK (optional, requires Speakeasy CLI)
cd spec-sdk-tests
./scripts/regenerate-sdk.sh

# Run spec-sdk-tests (RECOMMENDED - includes pre-flight checks)
cd spec-sdk-tests
rm -rf node_modules package-lock.json  # Clean install to avoid cache
npm install
./scripts/run-tests.sh

# OR run tests directly (skips validation checks)
npm test

# Test individual SDKs
cd sdks/outpost-go && go test ./...
cd sdks/outpost-python && pytest
cd sdks/outpost-typescript && npm test

# Start Outpost locally for testing
docker-compose -f build/dev/compose.yml up
# OR
go run cmd/outpost/main.go

# Verify Outpost is running
curl http://localhost:3333/healthz

# Clean caches if needed
npm cache clean --force
pip cache purge
```

### Key Files

- **OpenAPI Spec**: `docs/apis/openapi.yaml`
- **Generation Workflows**: 
  - `.github/workflows/sdk_generation_outpost_go.yaml`
  - `.github/workflows/sdk_generation_outpost_python.yaml`
  - `.github/workflows/sdk_generation_outpost_ts.yaml`
- **Publish Workflows**:
  - `.github/workflows/sdk_publish_outpost_go.yaml`
  - `.github/workflows/sdk_publish_outpost_python.yaml`
  - `.github/workflows/sdk_publish_outpost_ts.yaml`
- **SDK Test Suite**: `spec-sdk-tests/`
- **SDK Locations**:
  - `sdks/outpost-go/`
  - `sdks/outpost-python/`
  - `sdks/outpost-typescript/`

---

## Need Help?

- Check existing [Issues](https://github.com/hookdeck/outpost/issues) for similar problems
- Review [Speakeasy documentation](https://www.speakeasyapi.dev/docs)
- Ask in [GitHub Discussions](https://github.com/hookdeck/outpost/discussions)
- Contact the maintainers team

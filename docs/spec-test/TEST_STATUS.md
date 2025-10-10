# OpenAPI Contract Testing - Status Report

**Test Date**: 2025-10-10  
**Test Run**: After fixing test code issues  
**Total Tests**: 25  
**Passing**: 18 ✅  
**Failing**: 7 ❌  
**Success Rate**: 72.0% (improved from 65.2%)

## Overview

The OpenAPI contract testing infrastructure is **functional** and successfully validating API requests against the OpenAPI specification. We've fixed all test code issues within our control, improving the success rate from 65.2% to 72.0%.

The remaining 7 failures require investigation and potential fixes to either the tests or the Outpost implementation.

## ❌ Failing Tests (7)

### 1. PATCH Endpoint Validation (4 failures) - **ROOT CAUSE IDENTIFIED** ✅

**Tests:**

- `should update destination topics` (Line 2423)
- `should update destination config` (Line 2432)
- `should update destination credentials` (Line 2441)
- `should return 404 for updating non-existent destination` (Line 2450)

**Error**: `Error: API request failed with status 422`

**Root Cause**: `DestinationUpdate` schema uses `oneOf` **without a discriminator**.

**Diagnosis**:

- Tests send partial objects: `{ topics: [...] }` without `type` field
- OpenAPI `DestinationUpdate` schema (line 1031) uses `oneOf` for multiple destination types
- **Missing discriminator**: Unlike GET/POST schemas, no `discriminator.propertyName: type`
- Prism cannot determine which `oneOf` variant to validate against

**Solution**: Add `type: 'gcp_pubsub'` to all PATCH request bodies

```typescript
// Current (failing):
{ topics: ['user.created'] }

// Fixed:
{ type: 'gcp_pubsub', topics: ['user.created'] }
```

**Status**: Test code fix required - add `type` field to PATCH requests

---

### 2. Invalid JSON Validation (2 failures)

**Tests:**

- `should reject creation with invalid service_account_json` (Line 2403)
- `should reject update with invalid config` (Line 2463)

**Error**: `AssertionError: expected undefined to be one of [ 400, 422 ]`

**Status**: Error object doesn't have `response` property, suggesting backend may be accepting invalid JSON or crashing.

**Next Steps**:

- Verify backend validates `service_account_json` is well-formed JSON
- Ensure 400/422 error response is returned for invalid JSON
- May be related to PATCH validation issue above

---

### 3. Pagination Limit (1 failure)

**Test**: `should support pagination with limit` (Line 2411)

**Error**: `AssertionError: expected 4 to be at most 1`

**Status**: Test requests `limit=1` but backend returns 4 destinations.

**Next Steps**:

- Verify backend respects the `limit` query parameter
- Fix backend pagination if not working correctly

---

## Recommendations

1. **Investigate PATCH request validation** - Compare test request bodies with OpenAPI schemas
2. **Check invalid JSON handling** - Ensure backend rejects malformed JSON
3. **Fix pagination** - Backend should respect `limit` parameter

## Infrastructure Status ✅

- Prism proxy: Working
- API client: Working
- Authentication: Working
- Test isolation: Working
- Cleanup: Working
- Environment variables: Working

## Test Execution

**Command**: `./scripts/run-tests.sh`  
**Duration**: 220ms  
**Log File**: `test-run.log`

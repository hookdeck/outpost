# simplejsonmatch

Go port of [simple-json-match](https://github.com/hookdeck/simple-json-match) - a JSON schema matching library.

## Test Case Port

Tests are generated programmatically from the JS source using `generate_test.js`.

### Source

- **Original**: https://github.com/hookdeck/simple-json-match/blob/main/src/tests/index.test.ts
- **Go Port**: `match_test.go` (generated from `generate_test.js`)

### Test Counts

| Category | Count | Notes |
|----------|-------|-------|
| Main tests (implemented) | 108 | From JS `tests` array |
| Main tests (skipped) | 13 | `$ref` tests - not implemented |
| Not tests | 12 | From JS `not_tests` array |
| $not inversion | 108 | Each implemented test also run with `{$not: schema}` |
| **Total** | **241** | 108×2 + 12 + 13 skipped |

### Regenerating Tests

To regenerate `match_test.go` from the JS source:

```bash
node generate_test.js > match_test.go
```

## Operators Supported

| Operator | Description | Status |
|----------|-------------|--------|
| `$eq` | Deep equality | ✅ |
| `$neq` | Deep inequality | ✅ |
| `$gt` | Greater than | ✅ |
| `$gte` | Greater than or equal | ✅ |
| `$lt` | Less than | ✅ |
| `$lte` | Less than or equal | ✅ |
| `$in` | Substring or array membership | ✅ |
| `$nin` | Negated membership | ✅ |
| `$startsWith` | String prefix | ✅ |
| `$endsWith` | String suffix | ✅ |
| `$exist` | Field presence check | ✅ |
| `$or` | Logical OR | ✅ |
| `$and` | Logical AND | ✅ |
| `$not` | Logical NOT | ✅ |
| `$ref` | Field reference | ❌ Not implemented |

### Why `$ref` is not implemented

The `$ref` operator allows comparing a field's value against another field in the same document. It was omitted because:
- Limited use cases
- Adds complexity with JSONPath-like parsing

## Usage

```go
import "github.com/hookdeck/outpost/internal/simplejsonmatch"

// Basic matching
input := map[string]any{"type": "created", "count": 5}
schema := map[string]any{"type": "created", "count": map[string]any{"$gte": 1}}

if simplejsonmatch.Match(input, schema) {
    // Input matches the schema
}
```

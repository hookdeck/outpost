# GetTenantDestinationAttemptInclude

Fields to include in the response. Can be specified multiple times or comma-separated.
- `event`: Include event summary
- `event.data`: Include full event with payload data
- `response_data`: Include response body and headers



## Supported Types

### 

```go
getTenantDestinationAttemptInclude := operations.CreateGetTenantDestinationAttemptIncludeStr(string{/* values here */})
```

### 

```go
getTenantDestinationAttemptInclude := operations.CreateGetTenantDestinationAttemptIncludeArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch getTenantDestinationAttemptInclude.Type {
	case operations.GetTenantDestinationAttemptIncludeTypeStr:
		// getTenantDestinationAttemptInclude.Str is populated
	case operations.GetTenantDestinationAttemptIncludeTypeArrayOfStr:
		// getTenantDestinationAttemptInclude.ArrayOfStr is populated
}
```

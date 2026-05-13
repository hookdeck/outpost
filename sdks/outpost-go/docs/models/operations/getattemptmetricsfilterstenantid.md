# GetAttemptMetricsFiltersTenantID

Filter by tenant ID(s). Admin-only — rejected with 403 for JWT callers. Use bracket notation for multiple values (e.g., `filters[tenant_id][0]=t1&filters[tenant_id][1]=t2`).


## Supported Types

### 

```go
getAttemptMetricsFiltersTenantID := operations.CreateGetAttemptMetricsFiltersTenantIDStr(string{/* values here */})
```

### 

```go
getAttemptMetricsFiltersTenantID := operations.CreateGetAttemptMetricsFiltersTenantIDArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch getAttemptMetricsFiltersTenantID.Type {
	case operations.GetAttemptMetricsFiltersTenantIDTypeStr:
		// getAttemptMetricsFiltersTenantID.Str is populated
	case operations.GetAttemptMetricsFiltersTenantIDTypeArrayOfStr:
		// getAttemptMetricsFiltersTenantID.ArrayOfStr is populated
}
```

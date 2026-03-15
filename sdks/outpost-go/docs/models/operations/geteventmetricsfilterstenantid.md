# GetEventMetricsFiltersTenantID

Filter by tenant ID(s). Admin-only — rejected with 403 for JWT callers. Use bracket notation for multiple values (e.g., `filters[tenant_id][0]=t1&filters[tenant_id][1]=t2`).


## Supported Types

### 

```go
getEventMetricsFiltersTenantID := operations.CreateGetEventMetricsFiltersTenantIDStr(string{/* values here */})
```

### 

```go
getEventMetricsFiltersTenantID := operations.CreateGetEventMetricsFiltersTenantIDArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch getEventMetricsFiltersTenantID.Type {
	case operations.GetEventMetricsFiltersTenantIDTypeStr:
		// getEventMetricsFiltersTenantID.Str is populated
	case operations.GetEventMetricsFiltersTenantIDTypeArrayOfStr:
		// getEventMetricsFiltersTenantID.ArrayOfStr is populated
}
```

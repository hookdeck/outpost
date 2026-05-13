# GetAttemptMetricsFiltersDestinationID

Filter by destination ID(s). Use bracket notation for multiple values (e.g., `filters[destination_id][0]=d1&filters[destination_id][1]=d2`).


## Supported Types

### 

```go
getAttemptMetricsFiltersDestinationID := operations.CreateGetAttemptMetricsFiltersDestinationIDStr(string{/* values here */})
```

### 

```go
getAttemptMetricsFiltersDestinationID := operations.CreateGetAttemptMetricsFiltersDestinationIDArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch getAttemptMetricsFiltersDestinationID.Type {
	case operations.GetAttemptMetricsFiltersDestinationIDTypeStr:
		// getAttemptMetricsFiltersDestinationID.Str is populated
	case operations.GetAttemptMetricsFiltersDestinationIDTypeArrayOfStr:
		// getAttemptMetricsFiltersDestinationID.ArrayOfStr is populated
}
```

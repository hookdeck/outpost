# GetEventMetricsFiltersDestinationID

Filter by destination ID(s). Use bracket notation for multiple values (e.g., `filters[destination_id][0]=d1&filters[destination_id][1]=d2`).


## Supported Types

### 

```go
getEventMetricsFiltersDestinationID := operations.CreateGetEventMetricsFiltersDestinationIDStr(string{/* values here */})
```

### 

```go
getEventMetricsFiltersDestinationID := operations.CreateGetEventMetricsFiltersDestinationIDArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch getEventMetricsFiltersDestinationID.Type {
	case operations.GetEventMetricsFiltersDestinationIDTypeStr:
		// getEventMetricsFiltersDestinationID.Str is populated
	case operations.GetEventMetricsFiltersDestinationIDTypeArrayOfStr:
		// getEventMetricsFiltersDestinationID.ArrayOfStr is populated
}
```

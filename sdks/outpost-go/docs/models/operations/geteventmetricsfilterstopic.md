# GetEventMetricsFiltersTopic

Filter by topic name(s). Use bracket notation for multiple values (e.g., `filters[topic][0]=user.created&filters[topic][1]=user.updated`).


## Supported Types

### 

```go
getEventMetricsFiltersTopic := operations.CreateGetEventMetricsFiltersTopicStr(string{/* values here */})
```

### 

```go
getEventMetricsFiltersTopic := operations.CreateGetEventMetricsFiltersTopicArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch getEventMetricsFiltersTopic.Type {
	case operations.GetEventMetricsFiltersTopicTypeStr:
		// getEventMetricsFiltersTopic.Str is populated
	case operations.GetEventMetricsFiltersTopicTypeArrayOfStr:
		// getEventMetricsFiltersTopic.ArrayOfStr is populated
}
```

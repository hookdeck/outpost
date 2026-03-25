# GetAttemptMetricsFiltersTopic

Filter by topic name(s). Use bracket notation for multiple values (e.g., `filters[topic][0]=user.created&filters[topic][1]=user.updated`).


## Supported Types

### 

```go
getAttemptMetricsFiltersTopic := operations.CreateGetAttemptMetricsFiltersTopicStr(string{/* values here */})
```

### 

```go
getAttemptMetricsFiltersTopic := operations.CreateGetAttemptMetricsFiltersTopicArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch getAttemptMetricsFiltersTopic.Type {
	case operations.GetAttemptMetricsFiltersTopicTypeStr:
		// getAttemptMetricsFiltersTopic.Str is populated
	case operations.GetAttemptMetricsFiltersTopicTypeArrayOfStr:
		// getAttemptMetricsFiltersTopic.ArrayOfStr is populated
}
```

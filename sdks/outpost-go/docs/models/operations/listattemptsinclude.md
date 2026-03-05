# ListAttemptsInclude

Fields to include in the response. Can be specified multiple times or comma-separated.
- `event`: Include event summary (id, topic, time, eligible_for_retry, metadata)
- `event.data`: Include full event with payload data
- `response_data`: Include response body and headers



## Supported Types

### 

```go
listAttemptsInclude := operations.CreateListAttemptsIncludeStr(string{/* values here */})
```

### 

```go
listAttemptsInclude := operations.CreateListAttemptsIncludeArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch listAttemptsInclude.Type {
	case operations.ListAttemptsIncludeTypeStr:
		// listAttemptsInclude.Str is populated
	case operations.ListAttemptsIncludeTypeArrayOfStr:
		// listAttemptsInclude.ArrayOfStr is populated
}
```

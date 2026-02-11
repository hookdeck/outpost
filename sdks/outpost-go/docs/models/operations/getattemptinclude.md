# GetAttemptInclude

Fields to include in the response. Can be specified multiple times or comma-separated.
- `event`: Include event summary (id, topic, time, eligible_for_retry, metadata)
- `event.data`: Include full event with payload data
- `response_data`: Include response body and headers



## Supported Types

### 

```go
getAttemptInclude := operations.CreateGetAttemptIncludeStr(string{/* values here */})
```

### 

```go
getAttemptInclude := operations.CreateGetAttemptIncludeArrayOfStr([]string{/* values here */})
```


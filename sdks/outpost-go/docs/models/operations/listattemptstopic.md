# ListAttemptsTopic

Filter attempts by event topic(s). Can be specified multiple times or comma-separated.


## Supported Types

### 

```go
listAttemptsTopic := operations.CreateListAttemptsTopicStr(string{/* values here */})
```

### 

```go
listAttemptsTopic := operations.CreateListAttemptsTopicArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch listAttemptsTopic.Type {
	case operations.ListAttemptsTopicTypeStr:
		// listAttemptsTopic.Str is populated
	case operations.ListAttemptsTopicTypeArrayOfStr:
		// listAttemptsTopic.ArrayOfStr is populated
}
```

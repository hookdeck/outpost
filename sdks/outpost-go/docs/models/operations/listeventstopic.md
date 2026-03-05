# ListEventsTopic

Filter events by topic(s). Can be specified multiple times or comma-separated.


## Supported Types

### 

```go
listEventsTopic := operations.CreateListEventsTopicStr(string{/* values here */})
```

### 

```go
listEventsTopic := operations.CreateListEventsTopicArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch listEventsTopic.Type {
	case operations.ListEventsTopicTypeStr:
		// listEventsTopic.Str is populated
	case operations.ListEventsTopicTypeArrayOfStr:
		// listEventsTopic.ArrayOfStr is populated
}
```

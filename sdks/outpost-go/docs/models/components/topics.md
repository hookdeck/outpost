# Topics

"*" or an array of enabled topics. Topic strings can include "*" as a wildcard matching any run of characters. When available topics are configured, wildcard patterns must match at least one available topic.


## Supported Types

### TopicsEnum

```go
topics := components.CreateTopicsTopicsEnum(components.TopicsEnum{/* values here */})
```

### 

```go
topics := components.CreateTopicsArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch topics.Type {
	case components.TopicsTypeTopicsEnum:
		// topics.TopicsEnum is populated
	case components.TopicsTypeArrayOfStr:
		// topics.ArrayOfStr is populated
}
```

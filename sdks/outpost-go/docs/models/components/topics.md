# Topics

"*" or an array of enabled topics.


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

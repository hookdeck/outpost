# Topics

Filter destinations by supported topic(s).


## Supported Types

### 

```go
topics := operations.CreateTopicsStr(string{/* values here */})
```

### 

```go
topics := operations.CreateTopicsArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch topics.Type {
	case operations.TopicsTypeStr:
		// topics.Str is populated
	case operations.TopicsTypeArrayOfStr:
		// topics.ArrayOfStr is populated
}
```

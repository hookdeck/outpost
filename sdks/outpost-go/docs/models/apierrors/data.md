# Data

Additional error details. For validation errors, this is an array of human-readable messages.


## Supported Types

### 

```go
data := apierrors.CreateDataArrayOfStr([]string{/* values here */})
```

### 

```go
data := apierrors.CreateDataMapOfAny(map[string]any{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch data.Type {
	case apierrors.DataTypeArrayOfStr:
		// data.ArrayOfStr is populated
	case apierrors.DataTypeMapOfAny:
		// data.MapOfAny is populated
}
```

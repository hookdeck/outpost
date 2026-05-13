# FiltersCode

Filter by HTTP status code(s). Use bracket notation for multiple values (e.g., `filters[code][0]=200&filters[code][1]=500`).


## Supported Types

### 

```go
filtersCode := operations.CreateFiltersCodeStr(string{/* values here */})
```

### 

```go
filtersCode := operations.CreateFiltersCodeArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch filtersCode.Type {
	case operations.FiltersCodeTypeStr:
		// filtersCode.Str is populated
	case operations.FiltersCodeTypeArrayOfStr:
		// filtersCode.ArrayOfStr is populated
}
```

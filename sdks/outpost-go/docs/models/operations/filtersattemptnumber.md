# FiltersAttemptNumber

Filter by attempt number(s). Use bracket notation for multiple values (e.g., `filters[attempt_number][0]=1&filters[attempt_number][1]=2`).


## Supported Types

### 

```go
filtersAttemptNumber := operations.CreateFiltersAttemptNumberStr(string{/* values here */})
```

### 

```go
filtersAttemptNumber := operations.CreateFiltersAttemptNumberArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch filtersAttemptNumber.Type {
	case operations.FiltersAttemptNumberTypeStr:
		// filtersAttemptNumber.Str is populated
	case operations.FiltersAttemptNumberTypeArrayOfStr:
		// filtersAttemptNumber.ArrayOfStr is populated
}
```

# FiltersStatus

Filter by attempt status(es). Use bracket notation for multiple values (e.g., `filters[status][0]=success&filters[status][1]=failed`).


## Supported Types

### FiltersStatusEnum1

```go
filtersStatus := operations.CreateFiltersStatusFiltersStatusEnum1(operations.FiltersStatusEnum1{/* values here */})
```

### 

```go
filtersStatus := operations.CreateFiltersStatusArrayOfFiltersStatusEnum2([]operations.FiltersStatusEnum2{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch filtersStatus.Type {
	case operations.FiltersStatusTypeFiltersStatusEnum1:
		// filtersStatus.FiltersStatusEnum1 is populated
	case operations.FiltersStatusTypeArrayOfFiltersStatusEnum2:
		// filtersStatus.ArrayOfFiltersStatusEnum2 is populated
}
```

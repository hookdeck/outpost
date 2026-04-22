# FiltersDestinationType

Filter by destination type(s). Use bracket notation for multiple values (e.g., `filters[destination_type][0]=webhook&filters[destination_type][1]=aws_sqs`).


## Supported Types

### DestinationType

```go
filtersDestinationType := operations.CreateFiltersDestinationTypeDestinationType(components.DestinationType{/* values here */})
```

### 

```go
filtersDestinationType := operations.CreateFiltersDestinationTypeArrayOfDestinationType([]components.DestinationType{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch filtersDestinationType.Type {
	case operations.FiltersDestinationTypeTypeDestinationType:
		// filtersDestinationType.DestinationType is populated
	case operations.FiltersDestinationTypeTypeArrayOfDestinationType:
		// filtersDestinationType.ArrayOfDestinationType is populated
}
```

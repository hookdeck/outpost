# GetEventMetricsDimensionsUnion

Dimensions to group results by. Use bracket notation for multiple values (e.g., `dimensions[0]=topic&dimensions[1]=destination_id`).


## Supported Types

### GetEventMetricsDimensionsEnum1

```go
getEventMetricsDimensionsUnion := operations.CreateGetEventMetricsDimensionsUnionGetEventMetricsDimensionsEnum1(operations.GetEventMetricsDimensionsEnum1{/* values here */})
```

### 

```go
getEventMetricsDimensionsUnion := operations.CreateGetEventMetricsDimensionsUnionArrayOfGetEventMetricsDimensionsEnum2([]operations.GetEventMetricsDimensionsEnum2{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch getEventMetricsDimensionsUnion.Type {
	case operations.GetEventMetricsDimensionsUnionTypeGetEventMetricsDimensionsEnum1:
		// getEventMetricsDimensionsUnion.GetEventMetricsDimensionsEnum1 is populated
	case operations.GetEventMetricsDimensionsUnionTypeArrayOfGetEventMetricsDimensionsEnum2:
		// getEventMetricsDimensionsUnion.ArrayOfGetEventMetricsDimensionsEnum2 is populated
}
```

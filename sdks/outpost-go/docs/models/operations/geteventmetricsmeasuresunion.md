# GetEventMetricsMeasuresUnion

Measures to compute. At least one required. `rate` is events/second throughput. Use bracket notation for multiple values (e.g., `measures[0]=count`).


## Supported Types

### GetEventMetricsMeasuresEnum1

```go
getEventMetricsMeasuresUnion := operations.CreateGetEventMetricsMeasuresUnionGetEventMetricsMeasuresEnum1(operations.GetEventMetricsMeasuresEnum1{/* values here */})
```

### 

```go
getEventMetricsMeasuresUnion := operations.CreateGetEventMetricsMeasuresUnionArrayOfGetEventMetricsMeasuresEnum2([]operations.GetEventMetricsMeasuresEnum2{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch getEventMetricsMeasuresUnion.Type {
	case operations.GetEventMetricsMeasuresUnionTypeGetEventMetricsMeasuresEnum1:
		// getEventMetricsMeasuresUnion.GetEventMetricsMeasuresEnum1 is populated
	case operations.GetEventMetricsMeasuresUnionTypeArrayOfGetEventMetricsMeasuresEnum2:
		// getEventMetricsMeasuresUnion.ArrayOfGetEventMetricsMeasuresEnum2 is populated
}
```

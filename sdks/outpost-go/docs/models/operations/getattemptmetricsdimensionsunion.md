# GetAttemptMetricsDimensionsUnion

Dimensions to group results by. Use bracket notation for multiple values (e.g., `dimensions[0]=status&dimensions[1]=destination_id`).


## Supported Types

### GetAttemptMetricsDimensionsEnum1

```go
getAttemptMetricsDimensionsUnion := operations.CreateGetAttemptMetricsDimensionsUnionGetAttemptMetricsDimensionsEnum1(operations.GetAttemptMetricsDimensionsEnum1{/* values here */})
```

### 

```go
getAttemptMetricsDimensionsUnion := operations.CreateGetAttemptMetricsDimensionsUnionArrayOfGetAttemptMetricsDimensionsEnum2([]operations.GetAttemptMetricsDimensionsEnum2{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch getAttemptMetricsDimensionsUnion.Type {
	case operations.GetAttemptMetricsDimensionsUnionTypeGetAttemptMetricsDimensionsEnum1:
		// getAttemptMetricsDimensionsUnion.GetAttemptMetricsDimensionsEnum1 is populated
	case operations.GetAttemptMetricsDimensionsUnionTypeArrayOfGetAttemptMetricsDimensionsEnum2:
		// getAttemptMetricsDimensionsUnion.ArrayOfGetAttemptMetricsDimensionsEnum2 is populated
}
```

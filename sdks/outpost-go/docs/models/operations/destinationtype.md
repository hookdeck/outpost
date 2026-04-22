# DestinationType

Filter attempts by destination type(s). Use bracket notation for multiple values (e.g., `destination_type[0]=webhook&destination_type[1]=aws_sqs`).


## Supported Types

### DestinationType

```go
destinationType := operations.CreateDestinationTypeDestinationType(components.DestinationType{/* values here */})
```

### 

```go
destinationType := operations.CreateDestinationTypeArrayOfDestinationType([]components.DestinationType{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch destinationType.Type {
	case operations.DestinationTypeTypeDestinationType:
		// destinationType.DestinationType is populated
	case operations.DestinationTypeTypeArrayOfDestinationType:
		// destinationType.ArrayOfDestinationType is populated
}
```

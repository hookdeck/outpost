# ListTenantDestinationsType

Filter destinations by type(s).


## Supported Types

### DestinationType

```go
listTenantDestinationsType := operations.CreateListTenantDestinationsTypeDestinationType(components.DestinationType{/* values here */})
```

### 

```go
listTenantDestinationsType := operations.CreateListTenantDestinationsTypeArrayOfDestinationType([]components.DestinationType{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch listTenantDestinationsType.Type {
	case operations.ListTenantDestinationsTypeTypeDestinationType:
		// listTenantDestinationsType.DestinationType is populated
	case operations.ListTenantDestinationsTypeTypeArrayOfDestinationType:
		// listTenantDestinationsType.ArrayOfDestinationType is populated
}
```

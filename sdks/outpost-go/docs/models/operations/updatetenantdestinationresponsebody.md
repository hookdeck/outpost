# UpdateTenantDestinationResponseBody

Destination updated successfully or OAuth redirect needed.


## Supported Types

### Destination

```go
updateTenantDestinationResponseBody := operations.CreateUpdateTenantDestinationResponseBodyDestination(components.Destination{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch updateTenantDestinationResponseBody.Type {
	case operations.UpdateTenantDestinationResponseBodyTypeDestination:
		// updateTenantDestinationResponseBody.Destination is populated
}
```

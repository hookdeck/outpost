# DestinationID

Filter events by matched destination ID(s). Returns events that were routed to the specified destination(s). Use bracket notation for multiple values (e.g., `destination_id[0]=d1&destination_id[1]=d2`).


## Supported Types

### 

```go
destinationID := operations.CreateDestinationIDStr(string{/* values here */})
```

### 

```go
destinationID := operations.CreateDestinationIDArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch destinationID.Type {
	case operations.DestinationIDTypeStr:
		// destinationID.Str is populated
	case operations.DestinationIDTypeArrayOfStr:
		// destinationID.ArrayOfStr is populated
}
```

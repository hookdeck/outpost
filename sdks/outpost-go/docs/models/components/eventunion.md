# EventUnion

The associated event object. Only present when include=event or include=event.data.


## Supported Types

### EventSummary

```go
eventUnion := components.CreateEventUnionEventSummary(components.EventSummary{/* values here */})
```

### EventFull

```go
eventUnion := components.CreateEventUnionEventFull(components.EventFull{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch eventUnion.Type {
	case components.EventUnionTypeEventSummary:
		// eventUnion.EventSummary is populated
	case components.EventUnionTypeEventFull:
		// eventUnion.EventFull is populated
}
```

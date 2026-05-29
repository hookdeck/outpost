# EventUnion

The associated event object. Only present when include=event or include=event.data.


## Supported Types

### EventFull

```go
eventUnion := components.CreateEventUnionEventFull(components.EventFull{/* values here */})
```

### EventSummary

```go
eventUnion := components.CreateEventUnionEventSummary(components.EventSummary{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch eventUnion.Type {
	case components.EventUnionTypeEventFull:
		// eventUnion.EventFull is populated
	case components.EventUnionTypeEventSummary:
		// eventUnion.EventSummary is populated
}
```

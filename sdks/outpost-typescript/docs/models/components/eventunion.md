# EventUnion

The associated event object. Only present when include=event or include=event.data.


## Supported Types

### `components.EventSummary`

```typescript
const value: components.EventSummary = {
  id: "evt_123",
  tenantId: "tnt_123",
  destinationId: "des_456",
  topic: "user.created",
  time: new Date("2024-01-01T00:00:00Z"),
  eligibleForRetry: true,
  metadata: {
    "source": "crm",
  },
};
```

### `components.EventFull`

```typescript
const value: components.EventFull = {
  id: "evt_123",
  tenantId: "tnt_123",
  destinationId: "des_456",
  topic: "user.created",
  time: new Date("2024-01-01T00:00:00Z"),
  eligibleForRetry: true,
  metadata: {
    "source": "crm",
  },
  data: {
    "user_id": "userid",
    "status": "active",
  },
};
```


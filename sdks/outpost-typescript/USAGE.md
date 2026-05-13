<!-- Start SDK Example Usage [usage] -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const result = await outpost.publish({
    id: "evt_abc123xyz789",
    tenantId: "tenant_123",
    topic: "user.created",
    eligibleForRetry: true,
    metadata: {
      "source": "crm",
    },
    data: {
      "user_id": "userid",
      "status": "active",
    },
  });

  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->
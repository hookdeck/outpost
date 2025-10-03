<!-- Start SDK Example Usage [usage] -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost();

async function run() {
  const result = await outpost.health.check();

  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->
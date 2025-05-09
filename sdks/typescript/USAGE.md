<!-- Start SDK Example Usage [usage] -->
```typescript
import { SDK } from "openapi";

const sdk = new SDK();

async function run() {
  const result = await sdk.health.check();

  // Handle the result
  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->
# Reason

Present when the status is `degraded` or `failed`.
`startup_failed`: the worker has not been healthy since the process started (usually configuration or a dependency that is not up yet).
`recovery_failed`: the worker was healthy and then failed (e.g. a broker restart or failover).


## Example Usage

```typescript
import { Reason } from "@hookdeck/outpost-sdk/models/operations";

let value: Reason = "recovery_failed";
```

## Values

```typescript
"startup_failed" | "recovery_failed"
```
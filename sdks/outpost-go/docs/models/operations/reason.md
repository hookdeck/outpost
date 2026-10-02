# Reason

Present when the status is `degraded` or `failed`.
`startup_failed`: the worker has not been healthy since the process started (usually configuration or a dependency that is not up yet).
`recovery_failed`: the worker was healthy and then failed (e.g. a broker restart or failover).


## Example Usage

```go
import (
	"github.com/hookdeck/outpost/sdks/outpost-go/models/operations"
)

value := operations.ReasonStartupFailed
```


## Values

| Name                   | Value                  |
| ---------------------- | ---------------------- |
| `ReasonStartupFailed`  | startup_failed         |
| `ReasonRecoveryFailed` | recovery_failed        |
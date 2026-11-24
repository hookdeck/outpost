# Reason

Present when the status is `degraded` or `failed`.
`startup_failed`: the worker has not been healthy since the process started (usually configuration or a dependency that is not up yet).
`recovery_failed`: the worker was healthy and then failed (e.g. a broker restart or failover).


## Example Usage

```python
from outpost_sdk.models import Reason

value = Reason.STARTUP_FAILED
```


## Values

| Name              | Value             |
| ----------------- | ----------------- |
| `STARTUP_FAILED`  | startup_failed    |
| `RECOVERY_FAILED` | recovery_failed   |
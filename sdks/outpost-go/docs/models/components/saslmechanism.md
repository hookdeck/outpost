# SaslMechanism

SASL authentication mechanism.

## Example Usage

```go
import (
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
)

value := components.SaslMechanismPlain
```


## Values

| Name                       | Value                      |
| -------------------------- | -------------------------- |
| `SaslMechanismPlain`       | plain                      |
| `SaslMechanismScramSha256` | scram-sha-256              |
| `SaslMechanismScramSha512` | scram-sha-512              |
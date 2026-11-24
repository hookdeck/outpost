# KafkaConfigSaslMechanism

SASL authentication mechanism.

## Example Usage

```go
import (
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
)

value := components.KafkaConfigSaslMechanismPlain
```


## Values

| Name                                  | Value                                 |
| ------------------------------------- | ------------------------------------- |
| `KafkaConfigSaslMechanismPlain`       | plain                                 |
| `KafkaConfigSaslMechanismScramSha256` | scram-sha-256                         |
| `KafkaConfigSaslMechanismScramSha512` | scram-sha-512                         |
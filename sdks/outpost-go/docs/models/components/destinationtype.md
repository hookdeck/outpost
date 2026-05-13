# DestinationType

Type of destination.

## Example Usage

```go
import (
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
)

value := components.DestinationTypeWebhook
```


## Values

| Name                             | Value                            |
| -------------------------------- | -------------------------------- |
| `DestinationTypeWebhook`         | webhook                          |
| `DestinationTypeAwsSqs`          | aws_sqs                          |
| `DestinationTypeRabbitmq`        | rabbitmq                         |
| `DestinationTypeHookdeck`        | hookdeck                         |
| `DestinationTypeAwsKinesis`      | aws_kinesis                      |
| `DestinationTypeAzureServicebus` | azure_servicebus                 |
| `DestinationTypeAwsS3`           | aws_s3                           |
| `DestinationTypeGcpPubsub`       | gcp_pubsub                       |
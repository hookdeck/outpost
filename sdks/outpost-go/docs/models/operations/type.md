# Type

The type of the destination.

## Example Usage

```go
import (
	"github.com/hookdeck/outpost/sdks/outpost-go/models/operations"
)

value := operations.TypeWebhook
```


## Values

| Name                  | Value                 |
| --------------------- | --------------------- |
| `TypeWebhook`         | webhook               |
| `TypeAwsSqs`          | aws_sqs               |
| `TypeRabbitmq`        | rabbitmq              |
| `TypeHookdeck`        | hookdeck              |
| `TypeAwsKinesis`      | aws_kinesis           |
| `TypeAzureServicebus` | azure_servicebus      |
| `TypeAwsS3`           | aws_s3                |
| `TypeGcpPubsub`       | gcp_pubsub            |
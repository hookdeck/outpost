# GetDestinationTypeSchemaType

The type of the destination.

## Example Usage

```go
import (
	"github.com/hookdeck/outpost/sdks/outpost-go/models/operations"
)

value := operations.GetDestinationTypeSchemaTypeWebhook
```


## Values

| Name                                          | Value                                         |
| --------------------------------------------- | --------------------------------------------- |
| `GetDestinationTypeSchemaTypeWebhook`         | webhook                                       |
| `GetDestinationTypeSchemaTypeAwsSqs`          | aws_sqs                                       |
| `GetDestinationTypeSchemaTypeRabbitmq`        | rabbitmq                                      |
| `GetDestinationTypeSchemaTypeHookdeck`        | hookdeck                                      |
| `GetDestinationTypeSchemaTypeAwsKinesis`      | aws_kinesis                                   |
| `GetDestinationTypeSchemaTypeAzureServicebus` | azure_servicebus                              |
| `GetDestinationTypeSchemaTypeAwsS3`           | aws_s3                                        |
| `GetDestinationTypeSchemaTypeGcpPubsub`       | gcp_pubsub                                    |
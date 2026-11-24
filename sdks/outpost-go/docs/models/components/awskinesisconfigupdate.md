# AWSKinesisConfigUpdate

Partial AWS Kinesis config for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `StreamName`                                                                     | `*string`                                                                        | :heavy_minus_sign:                                                               | The name of the AWS Kinesis stream.                                              |
| `Region`                                                                         | `*string`                                                                        | :heavy_minus_sign:                                                               | The AWS region where the Kinesis stream is located.                              |
| `Endpoint`                                                                       | `*string`                                                                        | :heavy_minus_sign:                                                               | Optional. Custom AWS endpoint URL (e.g., for LocalStack or VPC endpoints).       |
| `PartitionKeyTemplate`                                                           | `*string`                                                                        | :heavy_minus_sign:                                                               | Optional. JMESPath template to extract the partition key from the event payload. |
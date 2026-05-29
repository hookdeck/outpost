# AWSKinesisConfigUpdate

Partial AWS Kinesis config for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `stream_name`                                                                    | *Optional[str]*                                                                  | :heavy_minus_sign:                                                               | The name of the AWS Kinesis stream.                                              |
| `region`                                                                         | *Optional[str]*                                                                  | :heavy_minus_sign:                                                               | The AWS region where the Kinesis stream is located.                              |
| `endpoint`                                                                       | *Optional[str]*                                                                  | :heavy_minus_sign:                                                               | Optional. Custom AWS endpoint URL (e.g., for LocalStack or VPC endpoints).       |
| `partition_key_template`                                                         | *Optional[str]*                                                                  | :heavy_minus_sign:                                                               | Optional. JMESPath template to extract the partition key from the event payload. |
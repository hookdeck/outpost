# AWSSQSConfigUpdate

Partial AWS SQS config for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                                                         | Type                                                                          | Required                                                                      | Description                                                                   | Example                                                                       |
| ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `endpoint`                                                                    | *Optional[str]*                                                               | :heavy_minus_sign:                                                            | Optional. Custom AWS endpoint URL (e.g., for LocalStack or specific regions). | https://sqs.us-east-1.amazonaws.com                                           |
| `queue_url`                                                                   | *Optional[str]*                                                               | :heavy_minus_sign:                                                            | The URL of the SQS queue.                                                     | https://sqs.us-east-1.amazonaws.com/123456789012/my-queue                     |
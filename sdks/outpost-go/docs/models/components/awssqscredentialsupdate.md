# AWSSQSCredentialsUpdate

Partial AWS SQS credentials for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                                   | Type                                                    | Required                                                | Description                                             |
| ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `Key`                                                   | `*string`                                               | :heavy_minus_sign:                                      | AWS Access Key ID.                                      |
| `Secret`                                                | `*string`                                               | :heavy_minus_sign:                                      | AWS Secret Access Key.                                  |
| `Session`                                               | `*string`                                               | :heavy_minus_sign:                                      | Optional AWS Session Token (for temporary credentials). |
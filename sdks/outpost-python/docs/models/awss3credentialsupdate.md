# Awss3CredentialsUpdate

Partial AWS S3 credentials for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                                   | Type                                                    | Required                                                | Description                                             |
| ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `key`                                                   | *Optional[str]*                                         | :heavy_minus_sign:                                      | AWS Access Key ID.                                      |
| `secret`                                                | *Optional[str]*                                         | :heavy_minus_sign:                                      | AWS Secret Access Key.                                  |
| `session`                                               | *Optional[str]*                                         | :heavy_minus_sign:                                      | Optional AWS Session Token (for temporary credentials). |
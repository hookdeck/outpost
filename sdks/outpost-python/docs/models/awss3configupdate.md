# Awss3ConfigUpdate

Partial AWS S3 config for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                              | Type                                               | Required                                           | Description                                        |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `bucket`                                           | *Optional[str]*                                    | :heavy_minus_sign:                                 | The name of your AWS S3 bucket.                    |
| `region`                                           | *Optional[str]*                                    | :heavy_minus_sign:                                 | The AWS region where your bucket is located.       |
| `key_template`                                     | *Optional[str]*                                    | :heavy_minus_sign:                                 | JMESPath expression for generating S3 object keys. |
| `storage_class`                                    | *Optional[str]*                                    | :heavy_minus_sign:                                 | The storage class for the S3 objects.              |
# Awss3ConfigUpdate

Partial AWS S3 config for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                              | Type                                               | Required                                           | Description                                        |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `Bucket`                                           | `*string`                                          | :heavy_minus_sign:                                 | The name of your AWS S3 bucket.                    |
| `Region`                                           | `*string`                                          | :heavy_minus_sign:                                 | The AWS region where your bucket is located.       |
| `KeyTemplate`                                      | `*string`                                          | :heavy_minus_sign:                                 | JMESPath expression for generating S3 object keys. |
| `StorageClass`                                     | `*string`                                          | :heavy_minus_sign:                                 | The storage class for the S3 objects.              |
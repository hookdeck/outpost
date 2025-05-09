# AWSKinesisCredentials

## Example Usage

```typescript
import { AWSKinesisCredentials } from "openapi/models/components";

let value: AWSKinesisCredentials = {
  key: "AKIAIOSFODNN7EXAMPLE",
  secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
  session: "AQoDYXdzEPT//////////wEXAMPLE...",
};
```

## Fields

| Field                                                   | Type                                                    | Required                                                | Description                                             | Example                                                 |
| ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `key`                                                   | *string*                                                | :heavy_check_mark:                                      | AWS Access Key ID.                                      | AKIAIOSFODNN7EXAMPLE                                    |
| `secret`                                                | *string*                                                | :heavy_check_mark:                                      | AWS Secret Access Key.                                  | wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY                |
| `session`                                               | *string*                                                | :heavy_minus_sign:                                      | Optional AWS Session Token (for temporary credentials). | AQoDYXdzEPT//////////wEXAMPLE...                        |
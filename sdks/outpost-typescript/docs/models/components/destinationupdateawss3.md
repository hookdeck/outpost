# DestinationUpdateAwss3

## Example Usage

```typescript
import { DestinationUpdateAwss3 } from "@hookdeck/outpost-sdk/models/components";

let value: DestinationUpdateAwss3 = {
  topics: "*",
  config: {
    bucket: "my-bucket",
    region: "us-east-1",
    keyTemplate:
      "join('/', [time.year, time.month, time.day, metadata.`\"event-id\"`, '.json'])",
    storageClass: "STANDARD",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    session: "AQoDYXdzEPT//////////wEXAMPLE...",
  },
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                | Example                                                                    |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `topics`                                                                   | *components.Topics*                                                        | :heavy_minus_sign:                                                         | "*" or an array of enabled topics.                                         | *                                                                          |
| `config`                                                                   | [components.Awss3Config](../../models/components/awss3config.md)           | :heavy_minus_sign:                                                         | N/A                                                                        |                                                                            |
| `credentials`                                                              | [components.Awss3Credentials](../../models/components/awss3credentials.md) | :heavy_minus_sign:                                                         | N/A                                                                        |                                                                            |
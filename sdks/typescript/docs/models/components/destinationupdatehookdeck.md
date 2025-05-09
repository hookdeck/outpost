# DestinationUpdateHookdeck

## Example Usage

```typescript
import { DestinationUpdateHookdeck } from "openapi/models/components";

let value: DestinationUpdateHookdeck = {
  topics: "*",
  credentials: {
    token: "hd_token_...",
  },
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      | Example                                                                          |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `topics`                                                                         | *components.Topics*                                                              | :heavy_minus_sign:                                                               | "*" or an array of enabled topics.                                               | *                                                                                |
| `config`                                                                         | *any*                                                                            | :heavy_minus_sign:                                                               | N/A                                                                              |                                                                                  |
| `credentials`                                                                    | [components.HookdeckCredentials](../../models/components/hookdeckcredentials.md) | :heavy_minus_sign:                                                               | N/A                                                                              |                                                                                  |
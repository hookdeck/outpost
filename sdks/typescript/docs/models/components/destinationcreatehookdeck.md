# DestinationCreateHookdeck

## Example Usage

```typescript
import { DestinationCreateHookdeck } from "openapi/models/components";

let value: DestinationCreateHookdeck = {
  id: "user-provided-id",
  type: "hookdeck",
  topics: "*",
  credentials: {
    token: "hd_token_...",
  },
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          | Example                                                                                              |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `id`                                                                                                 | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Optional user-provided ID. A UUID will be generated if empty.                                        | user-provided-id                                                                                     |
| `type`                                                                                               | [components.DestinationCreateHookdeckType](../../models/components/destinationcreatehookdecktype.md) | :heavy_check_mark:                                                                                   | Type of the destination. Must be 'hookdeck'.                                                         |                                                                                                      |
| `topics`                                                                                             | *components.Topics*                                                                                  | :heavy_check_mark:                                                                                   | "*" or an array of enabled topics.                                                                   | *                                                                                                    |
| `config`                                                                                             | *any*                                                                                                | :heavy_minus_sign:                                                                                   | N/A                                                                                                  |                                                                                                      |
| `credentials`                                                                                        | [components.HookdeckCredentials](../../models/components/hookdeckcredentials.md)                     | :heavy_check_mark:                                                                                   | N/A                                                                                                  |                                                                                                      |
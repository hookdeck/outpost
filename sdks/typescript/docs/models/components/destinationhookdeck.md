# DestinationHookdeck

## Example Usage

```typescript
import { DestinationHookdeck } from "openapi/models/components";

let value: DestinationHookdeck = {
  id: "des_hkd_abc",
  type: "hookdeck",
  topics: [
    "*",
  ],
  disabledAt: null,
  createdAt: new Date("2024-04-01T10:00:00Z"),
  config: {},
  credentials: {
    token: "hd_token_...",
  },
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | Control plane generated ID or user provided ID for the destination.                           | des_12345                                                                                     |
| `type`                                                                                        | [components.DestinationHookdeckType](../../models/components/destinationhookdecktype.md)      | :heavy_check_mark:                                                                            | Type of the destination.                                                                      | hookdeck                                                                                      |
| `topics`                                                                                      | *components.Topics*                                                                           | :heavy_check_mark:                                                                            | "*" or an array of enabled topics.                                                            | *                                                                                             |
| `disabledAt`                                                                                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | ISO Date when the destination was disabled, or null if enabled.                               | <nil>                                                                                         |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | ISO Date when the destination was created.                                                    | 2024-01-01T00:00:00Z                                                                          |
| `config`                                                                                      | *any*                                                                                         | :heavy_minus_sign:                                                                            | N/A                                                                                           |                                                                                               |
| `credentials`                                                                                 | [components.HookdeckCredentials](../../models/components/hookdeckcredentials.md)              | :heavy_check_mark:                                                                            | N/A                                                                                           |                                                                                               |
| `target`                                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | A human-readable representation of the destination target (Hookdeck). Read-only.              | Hookdeck                                                                                      |
| `targetUrl`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | A URL link to the destination target (e.g., Hookdeck dashboard). Read-only.                   | https://dashboard.hookdeck.com/sources/src_xxxyyyzzz                                          |
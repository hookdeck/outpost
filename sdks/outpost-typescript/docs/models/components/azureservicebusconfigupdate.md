# AzureServiceBusConfigUpdate

Partial Azure Service Bus config for PATCH updates (RFC 7396 merge-patch).

## Example Usage

```typescript
import { AzureServiceBusConfigUpdate } from "@hookdeck/outpost-sdk/models/components";

let value: AzureServiceBusConfigUpdate = {};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `name`                                                                   | *string*                                                                 | :heavy_minus_sign:                                                       | The name of the Azure Service Bus queue or topic to publish messages to. |
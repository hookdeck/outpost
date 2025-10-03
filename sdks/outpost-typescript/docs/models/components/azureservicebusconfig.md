# AzureServiceBusConfig

## Example Usage

```typescript
import { AzureServiceBusConfig } from "@hookdeck/outpost-sdk/models/components";

let value: AzureServiceBusConfig = {
  name: "my-queue-or-topic",
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              | Example                                                                  |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `name`                                                                   | *string*                                                                 | :heavy_check_mark:                                                       | The name of the Azure Service Bus queue or topic to publish messages to. | my-queue-or-topic                                                        |
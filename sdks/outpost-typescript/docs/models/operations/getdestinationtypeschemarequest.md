# GetDestinationTypeSchemaRequest

## Example Usage

```typescript
import { GetDestinationTypeSchemaRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: GetDestinationTypeSchemaRequest = {
  type: "aws_s3",
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `type`                                                                                             | [operations.GetDestinationTypeSchemaType](../../models/operations/getdestinationtypeschematype.md) | :heavy_check_mark:                                                                                 | The type of the destination.                                                                       |
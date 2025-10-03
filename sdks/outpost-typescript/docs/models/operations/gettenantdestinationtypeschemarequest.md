# GetTenantDestinationTypeSchemaRequest

## Example Usage

```typescript
import { GetTenantDestinationTypeSchemaRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: GetTenantDestinationTypeSchemaRequest = {
  type: "aws_kinesis",
};
```

## Fields

| Field                                                                                                          | Type                                                                                                           | Required                                                                                                       | Description                                                                                                    |
| -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `tenantId`                                                                                                     | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | The ID of the tenant. Required when using AdminApiKey authentication.                                          |
| `type`                                                                                                         | [operations.GetTenantDestinationTypeSchemaType](../../models/operations/gettenantdestinationtypeschematype.md) | :heavy_check_mark:                                                                                             | The type of the destination.                                                                                   |
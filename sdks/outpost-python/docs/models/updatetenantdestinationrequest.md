# UpdateTenantDestinationRequest


## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `params`                                                              | [models.DestinationUpdate](../models/destinationupdate.md)            | :heavy_check_mark:                                                    | N/A                                                                   |
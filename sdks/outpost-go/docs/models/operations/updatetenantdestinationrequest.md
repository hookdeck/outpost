# UpdateTenantDestinationRequest


## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `TenantID`                                                                   | **string*                                                                    | :heavy_minus_sign:                                                           | The ID of the tenant. Required when using AdminApiKey authentication.        |
| `DestinationID`                                                              | *string*                                                                     | :heavy_check_mark:                                                           | The ID of the destination.                                                   |
| `Params`                                                                     | [components.DestinationUpdate](../../models/components/destinationupdate.md) | :heavy_check_mark:                                                           | N/A                                                                          |
# RetryRequest

Request body for retrying event delivery to a destination.


## Fields

| Field                                    | Type                                     | Required                                 | Description                              | Example                                  |
| ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- |
| `EventID`                                | *string*                                 | :heavy_check_mark:                       | The ID of the event to retry.            | evt_123                                  |
| `DestinationID`                          | *string*                                 | :heavy_check_mark:                       | The ID of the destination to deliver to. | des_456                                  |
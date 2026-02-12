# RetryRequest

Request body for retrying event delivery to a destination.


## Fields

| Field                                    | Type                                     | Required                                 | Description                              | Example                                  |
| ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- |
| `event_id`                               | *str*                                    | :heavy_check_mark:                       | The ID of the event to retry.            | evt_123                                  |
| `destination_id`                         | *str*                                    | :heavy_check_mark:                       | The ID of the destination to deliver to. | des_456                                  |
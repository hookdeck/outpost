# EventFull

Full event object with data (returned when include=event.data).


## Fields

| Field                                        | Type                                         | Required                                     | Description                                  | Example                                      |
| -------------------------------------------- | -------------------------------------------- | -------------------------------------------- | -------------------------------------------- | -------------------------------------------- |
| `ID`                                         | **string*                                    | :heavy_minus_sign:                           | N/A                                          | evt_123                                      |
| `TenantID`                                   | **string*                                    | :heavy_minus_sign:                           | The tenant this event belongs to.            | tnt_123                                      |
| `DestinationID`                              | **string*                                    | :heavy_minus_sign:                           | The destination this event was delivered to. | des_456                                      |
| `Topic`                                      | **string*                                    | :heavy_minus_sign:                           | N/A                                          | user.created                                 |
| `Time`                                       | [*time.Time](https://pkg.go.dev/time#Time)   | :heavy_minus_sign:                           | Time the event was received.                 | 2024-01-01T00:00:00Z                         |
| `EligibleForRetry`                           | **bool*                                      | :heavy_minus_sign:                           | Whether this event can be retried.           | true                                         |
| `Metadata`                                   | map[string]*string*                          | :heavy_minus_sign:                           | N/A                                          | {<br/>"source": "crm"<br/>}                  |
| `Data`                                       | map[string]*any*                             | :heavy_minus_sign:                           | The event payload data.                      | {<br/>"user_id": "userid",<br/>"status": "active"<br/>} |
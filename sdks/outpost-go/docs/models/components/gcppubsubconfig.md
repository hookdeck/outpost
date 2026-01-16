# GCPPubSubConfig


## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        | Example                                                            |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `ProjectID`                                                        | *string*                                                           | :heavy_check_mark:                                                 | The GCP project ID.                                                | my-project-123                                                     |
| `Topic`                                                            | *string*                                                           | :heavy_check_mark:                                                 | The Pub/Sub topic name.                                            | events-topic                                                       |
| `Endpoint`                                                         | **string*                                                          | :heavy_minus_sign:                                                 | Optional. Custom endpoint URL (e.g., localhost:8085 for emulator). | pubsub.googleapis.com:443                                          |
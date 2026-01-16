# GCPPubSubConfig


## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        | Example                                                            |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `project_id`                                                       | *str*                                                              | :heavy_check_mark:                                                 | The GCP project ID.                                                | my-project-123                                                     |
| `topic`                                                            | *str*                                                              | :heavy_check_mark:                                                 | The Pub/Sub topic name.                                            | events-topic                                                       |
| `endpoint`                                                         | *Optional[str]*                                                    | :heavy_minus_sign:                                                 | Optional. Custom endpoint URL (e.g., localhost:8085 for emulator). | pubsub.googleapis.com:443                                          |
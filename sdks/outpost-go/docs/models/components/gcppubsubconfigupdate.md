# GCPPubSubConfigUpdate

Partial GCP Pub/Sub config for PATCH updates (RFC 7396 merge-patch).


## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `ProjectID`                                                        | `*string`                                                          | :heavy_minus_sign:                                                 | The GCP project ID.                                                |
| `Topic`                                                            | `*string`                                                          | :heavy_minus_sign:                                                 | The Pub/Sub topic name.                                            |
| `Endpoint`                                                         | `*string`                                                          | :heavy_minus_sign:                                                 | Optional. Custom endpoint URL (e.g., localhost:8085 for emulator). |
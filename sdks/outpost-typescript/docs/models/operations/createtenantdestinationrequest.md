# CreateTenantDestinationRequest

## Example Usage

```typescript
import { CreateTenantDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: CreateTenantDestinationRequest = {
  tenantId: "<id>",
  body: {
    type: "gcp_pubsub",
    topics: "*",
    config: {
      projectId: "my-project-123",
      topic: "events-topic",
      endpoint: "pubsub.googleapis.com:443",
    },
    credentials: {
      serviceAccountJson:
        "{\"type\":\"service_account\",\"project_id\":\"my-project\",\"private_key_id\":\"key123\",\"private_key\":\"-----BEGIN PRIVATE KEY-----\\n...\\n-----END PRIVATE KEY-----\\n\",\"client_email\":\"my-service@my-project.iam.gserviceaccount.com\"}",
    },
  },
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `body`                                                                | *components.DestinationCreate*                                        | :heavy_check_mark:                                                    | N/A                                                                   |
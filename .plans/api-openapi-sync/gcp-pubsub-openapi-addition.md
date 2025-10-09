# GCP Pub/Sub OpenAPI Schema Addition

## Quick Instructions

Add the following schemas to your `docs/apis/openapi.yaml` file at **line 287** (right after the AWSS3Credentials schema and before the "Type-Specific Destination Schemas" comment):

```yaml
    GCPPubSubConfig:
      type: object
      required: [project_id, topic]
      properties:
        project_id:
          type: string
          description: The GCP project ID.
          example: "my-project-123"
        topic:
          type: string
          description: The Pub/Sub topic name.
          example: "events-topic"
        endpoint:
          type: string
          description: Optional. Custom endpoint URL (e.g., localhost:8085 for emulator).
          example: "pubsub.googleapis.com:443"
    GCPPubSubCredentials:
      type: object
      required: [service_account_json]
      properties:
        service_account_json:
          type: string
          description: Service account key JSON. The entire JSON key file content as a string.
          example: '{"type":"service_account","project_id":"my-project","private_key_id":"key123","private_key":"-----BEGIN PRIVATE KEY-----\\n...\\n-----END PRIVATE KEY-----\\n","client_email":"my-service@my-project.iam.gserviceaccount.com"}'

```

## Also Add the Destination Response Schema

You should also add a destination response schema for GCP Pub/Sub. Look for where the other destination-specific response schemas are defined (around line 500-600) and add:

```yaml
    DestinationGCPPubSub:
      type: object
      # Properties duplicated from DestinationBase
      required: [id, type, topics, config, credentials, created_at, disabled_at]
      properties:
        id:
          type: string
          description: Control plane generated ID or user provided ID for the destination.
          example: "des_12345"
        type:
          type: string
          description: Type of the destination.
          enum: [gcp_pubsub]
          example: "gcp_pubsub"
        topics:
          $ref: "#/components/schemas/Topics"
        disabled_at:
          type: string
          format: date-time
          nullable: true
          description: ISO Date when the destination was disabled, or null if enabled.
          example: null
        created_at:
          type: string
          format: date-time
          description: ISO Date when the destination was created.
          example: "2024-01-01T00:00:00Z"
        config:
          $ref: "#/components/schemas/GCPPubSubConfig"
        credentials:
          $ref: "#/components/schemas/GCPPubSubCredentials"
        target:
          type: string
          description: A human-readable representation of the destination target (project/topic). Read-only.
          readOnly: true
          example: "my-project-123/events-topic"
        target_url:
          type: string
          format: url
          nullable: true
          description: A URL link to the destination target (GCP Console link to the topic). Read-only.
          readOnly: true
          example: "https://console.cloud.google.com/cloudpubsub/topic/detail/events-topic?project=my-project-123"
      example:
        id: "des_gcp_pubsub_123"
        type: "gcp_pubsub"
        topics: ["order.created", "order.updated"]
        disabled_at: null
        created_at: "2024-03-10T14:30:00Z"
        config:
          project_id: "my-project-123"
          topic: "events-topic"
        credentials:
          service_account_json: '{"type":"service_account",...}'
```

## Quick Copy & Paste

If you want to add this quickly:

1. Open `docs/apis/openapi.yaml`
2. Find line 287 (after AWSS3Credentials)
3. Paste the GCPPubSubConfig and GCPPubSubCredentials schemas
4. Find the section with other DestinationXXX schemas (around line 500-600)
5. Add the DestinationGCPPubSub schema

## Context

The GCP Pub/Sub destination is already implemented in the codebase at:
- `internal/destregistry/providers/destgcppubsub/`
- Metadata at: `internal/destregistry/metadata/providers/gcp_pubsub/metadata.json`

This addition to the OpenAPI spec documents the existing functionality, matching the patterns used for other destinations like AWS SQS, AWS Kinesis, etc.

## Fields Explained

### Config Fields:
- **project_id** (required): The GCP project ID where the Pub/Sub topic exists
- **topic** (required): The name of the Pub/Sub topic to publish to
- **endpoint** (optional): For testing with emulator or custom endpoints

### Credential Fields:
- **service_account_json** (required, sensitive): The complete service account JSON key file content as a string

## Testing

After adding these schemas, you can validate the OpenAPI spec using:
```bash
# Install a validator if you don't have one
npm install -g @apidevtools/swagger-cli

# Validate the spec
swagger-cli validate docs/apis/openapi.yaml
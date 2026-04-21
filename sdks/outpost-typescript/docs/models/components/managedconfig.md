# ManagedConfig

Managed configuration values for Outpost Cloud.
This API is available only on the managed version.
Self-hosted deployments configure these values using environment variables.


## Example Usage

```typescript
import { ManagedConfig } from "@hookdeck/outpost-sdk/models/components";

let value: ManagedConfig = {
  destinationsWebhookMode: "default",
  topics: "user.created,user.updated",
};
```

## Fields

| Field                                              | Type                                               | Required                                           | Description                                        |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `alertAutoDisableDestination`                      | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `alertCallbackUrl`                                 | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `alertConsecutiveFailureCount`                     | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `deliveryTimeoutSeconds`                           | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsAwsKinesisMetadataInPayload`          | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsIncludeMillisecondTimestamp`          | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookDisableDefaultEventIdHeader`   | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookDisableDefaultSignatureHeader` | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookDisableDefaultTimestampHeader` | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookDisableDefaultTopicHeader`     | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookHeaderPrefix`                  | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookMode`                          | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookProxyUrl`                      | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookSignatureAlgorithm`            | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookSignatureContentTemplate`      | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookSignatureEncoding`             | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookSignatureHeaderTemplate`       | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `destinationsWebhookSigningSecretTemplate`         | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `httpUserAgent`                                    | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `idgenAttemptPrefix`                               | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `idgenDestinationPrefix`                           | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `idgenEventPrefix`                                 | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `idgenType`                                        | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `maxDestinationsPerTenant`                         | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `organizationName`                                 | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `portalBrandColor`                                 | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `portalDisableOutpostBranding`                     | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `portalFaviconUrl`                                 | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `portalForceTheme`                                 | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `portalLogo`                                       | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `portalLogoDark`                                   | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `portalOrganizationName`                           | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `portalRefererUrl`                                 | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `portalRefreshUrl`                                 | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `portalEnableDestinationFilter`                    | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `portalEnableWebhookCustomHeaders`                 | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `retryIntervalSeconds`                             | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `maxRetryLimit`                                    | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `retrySchedule`                                    | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `otelExporterOtlpEndpoint`                         | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `otelExporterOtlpHeaders`                          | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `otelExporterOtlpProtocol`                         | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `otelExporterOtlpTracesEndpoint`                   | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `otelExporterOtlpMetricsEndpoint`                  | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `otelExporterOtlpLogsEndpoint`                     | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `otelServiceName`                                  | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishAwsSqsAccessKeyId`                         | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishAwsSqsEndpoint`                            | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishAwsSqsQueue`                               | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishAwsSqsRegion`                              | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishAwsSqsSecretAccessKey`                     | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishAzureServicebusConnectionString`           | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishAzureServicebusSubscription`               | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishAzureServicebusTopic`                      | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishGcpPubsubProject`                          | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishGcpPubsubServiceAccountCredentials`        | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishGcpPubsubSubscription`                     | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishGcpPubsubTopic`                            | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishRabbitmqExchange`                          | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishRabbitmqQueue`                             | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `publishRabbitmqServerUrl`                         | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
| `topics`                                           | *string*                                           | :heavy_minus_sign:                                 | N/A                                                |
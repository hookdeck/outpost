# ManagedConfig

Managed configuration values for Outpost Cloud.
This API is available only on the managed version.
Self-hosted deployments configure these values using environment variables.



## Fields

| Field                                              | Type                                               | Required                                           | Description                                        |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `AlertAutoDisableDestination`                      | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `AlertCallbackURL`                                 | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `AlertConsecutiveFailureCount`                     | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DeliveryTimeoutSeconds`                           | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsAwsKinesisMetadataInPayload`          | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsIncludeMillisecondTimestamp`          | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookDisableDefaultEventIDHeader`   | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookDisableDefaultSignatureHeader` | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookDisableDefaultTimestampHeader` | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookDisableDefaultTopicHeader`     | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookHeaderPrefix`                  | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookMode`                          | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookProxyURL`                      | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookSignatureAlgorithm`            | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookSignatureContentTemplate`      | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookSignatureEncoding`             | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookSignatureHeaderTemplate`       | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `DestinationsWebhookSigningSecretTemplate`         | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `HTTPUserAgent`                                    | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `IdgenAttemptPrefix`                               | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `IdgenDestinationPrefix`                           | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `IdgenEventPrefix`                                 | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `IdgenType`                                        | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `MaxDestinationsPerTenant`                         | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `OrganizationName`                                 | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PortalBrandColor`                                 | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PortalDisableOutpostBranding`                     | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PortalFaviconURL`                                 | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PortalForceTheme`                                 | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PortalLogo`                                       | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PortalLogoDark`                                   | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PortalOrganizationName`                           | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PortalRefererURL`                                 | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PortalRefreshURL`                                 | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PortalEnableDestinationFilter`                    | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PortalEnableWebhookCustomHeaders`                 | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `RetryIntervalSeconds`                             | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `MaxRetryLimit`                                    | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `RetrySchedule`                                    | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `OtelExporterOtlpEndpoint`                         | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `OtelExporterOtlpHeaders`                          | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `OtelExporterOtlpProtocol`                         | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `OtelExporterOtlpTracesEndpoint`                   | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `OtelExporterOtlpMetricsEndpoint`                  | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `OtelExporterOtlpLogsEndpoint`                     | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `OtelServiceName`                                  | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishAwsSqsAccessKeyID`                         | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishAwsSqsEndpoint`                            | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishAwsSqsQueue`                               | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishAwsSqsRegion`                              | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishAwsSqsSecretAccessKey`                     | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishAzureServicebusConnectionString`           | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishAzureServicebusSubscription`               | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishAzureServicebusTopic`                      | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishGcpPubsubProject`                          | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishGcpPubsubServiceAccountCredentials`        | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishGcpPubsubSubscription`                     | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishGcpPubsubTopic`                            | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishRabbitmqExchange`                          | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishRabbitmqQueue`                             | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `PublishRabbitmqServerURL`                         | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
| `Topics`                                           | `*string`                                          | :heavy_minus_sign:                                 | N/A                                                |
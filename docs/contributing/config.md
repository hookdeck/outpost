# Config

TBD. This document should go into more details about various Outpost configuration.

## MQs

TBD

## OpenTelemetry

To support OpenTelemetry, you must have this env `OTEL_SERVICE_NAME`. Its value is your service name when sending to OTEL. You can use whichever value you see fit.

In production, if Outpost is run as a singular service, then the service name can be `outpost`. If Outpost is run in multiple processes (for API, delivery, log, etc.), you can provide more granularity by including the service type such as `outpost.api` or `outpost.delivery`, etc. Ultimately, it's up to the end users which value they want to see in their telemetry data.

Besides `OTEL_SERVICE_NAME`, we support the official [OpenTelemetry Environment Variable Specification](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/).

To specify the exporter endpoint, you can use `OTEL_EXPORTER_OTLP_ENDPOINT` or individual exporters such as `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`, or `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`.

By default, Outpost will export all Telemetry data. You can disable specific telemetry by setting its exporter to `none`. For example, if you only want to receive traces & metrics:

```
OTEL_TRACES_EXPORTER="otlp" # default
OTEL_METRICS_EXPORTER="otlp" # default
OTEL_LOGS_EXPORTER="none" # disable logs
```

Currently, we only support `otlp` exporter. If you have specific needs for other exporter configuration (like Prometheus), you must set up your own OTEL collector and configure it accordingly.

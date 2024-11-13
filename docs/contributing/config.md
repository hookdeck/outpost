# Config

TBD. This document should go into more details about various Outpost configuration.

## MQs

TBD

## OpenTelemetry

To support OpenTelemetry, you must have this env `OTEL_SERVICE_NAME`. Its value is your service name when sending to OTEL. You can use whichever value you see fit.

In production, if Outpost is run as a singular service, then the service name can be `outpost`. If Outpost is run in multiple processes (for API, delivery, log, etc.), you can provide more granularity by including the service type such as `outpost.api` or `outpost.delivery`, etc. Ultimately, it's up to the end users which value they want to see in their telemetry data.

Besides `OTEL_SERVICE_NAME`, we support the official [OpenTelemetry Environment Variable Specification](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/).

With OTEL, there are 3 telemetry data: traces, metrics, and logs. By default, Outpost will export all of them. You can provide a generic `OTEL_EXPORTER_OTLP_ENDPOINT` in that case.

If you don't want to receive all telemetry, you must specify which data to receive by specifying the exporter URL individually. For example, if you only want to receive only traces & metrics:

✅ you MUST specify `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` and `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`
❌ you MUST not provide `OTEL_EXPORTER_OTLP_ENDPOINT` nor `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`

# Uptrace Setup for local OpenTelemetry development

This is a simplified version of the official [Uptrace's Docker example](https://github.com/uptrace/uptrace/blob/master/example/docker/README.md). This setup removed all example data and just set up a minimum installation of Uptrace.

## Start Uptrace

**Step 1**. Start the services with Docker

```sh
# from hookdeck/EventKit/configs/dev/uptrace

# Pull required images
$ docker compose pull
$ docker compose up -d
```

**Step 2**. Make sure Uptrace is running

```sh
$ docker compose logs uptrace
```

**Step 3**. Open Uptrace UI at [http://localhost:14318](http://localhost:14318) with this credentails

```
uptrace@localhost
uptrace
```

## Usage with EventKit

With Uptrace, the convention is the first project is to monitor Uptrace itself. In our configuration, EventKit will be the 2nd project. There's a way to switch project in Uptrace dashboard in the sidebar. Make sure you're on the right project before proceeding.

Here's the environment variables you need to set for EventKit to send telemetry data to Uptrace:

```
OTEL_SERVICE_NAME=eventkit
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT="localhost:14317"
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT="localhost:14317"
OTEL_EXPORTER_OTLP_LOGS_ENDPOINT="localhost:14317"
```

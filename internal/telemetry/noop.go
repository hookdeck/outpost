package telemetry

import "context"

type NoopTelemetry struct{}

func (t *NoopTelemetry) Init(ctx context.Context) {}

func (t *NoopTelemetry) Flush() {}

func (t *NoopTelemetry) ApplicationStarted(ctx context.Context, application ApplicationInfo) {}

func (t *NoopTelemetry) DestinationCreated(ctx context.Context, destinationType string) {}

func (t *NoopTelemetry) TenantCreated(ctx context.Context) {}

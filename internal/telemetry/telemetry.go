package telemetry

import "context"

type Telemetry interface {
	Init(ctx context.Context)
	Flush()

	// Events
	ApplicationStarted(ctx context.Context, application ApplicationInfo)
	DestinationCreated(ctx context.Context, destinationType string)
	TenantCreated(ctx context.Context)
}

func New(enabled bool) Telemetry {
	if !enabled {
		return &NoopTelemetry{}
	}
	return &telemetryImpl{}
}

type telemetryImpl struct {
}

func (t *telemetryImpl) Init(ctx context.Context) {
}

func (t *telemetryImpl) Flush() {
}

func (t *telemetryImpl) ApplicationStarted(ctx context.Context, application ApplicationInfo) {
}

func (t *telemetryImpl) DestinationCreated(ctx context.Context, destinationType string) {
}

func (t *telemetryImpl) TenantCreated(ctx context.Context) {
}

type ApplicationInfo struct {
	Version       string
	MQ            string
	PortalEnabled string
}

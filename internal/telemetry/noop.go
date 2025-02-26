package telemetry

import (
	"context"

	"github.com/gin-gonic/gin"
)

type NoopTelemetry struct{}

func (t *NoopTelemetry) Init(ctx context.Context) {}

func (t *NoopTelemetry) Flush() {}

func (t *NoopTelemetry) MakeSentryHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

func (t *NoopTelemetry) ApplicationStarted(ctx context.Context, application ApplicationInfo) {}

func (t *NoopTelemetry) DestinationCreated(ctx context.Context, destinationType string) {}

func (t *NoopTelemetry) TenantCreated(ctx context.Context) {}

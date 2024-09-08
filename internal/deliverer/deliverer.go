package deliverer

import (
	"context"

	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type Delivery struct {
	logger *otelzap.Logger
}

func New(logger *otelzap.Logger) *Delivery {
	return &Delivery{logger}
}

func (d *Delivery) Deliver(ctx context.Context, destination *models.Destination, event *ingest.Event) error {
	logger := d.logger.Ctx(ctx)

	logger.Info("delivering event",
		zap.String("event_id", event.ID),
		zap.String("tenant_id", destination.TenantID),
		zap.String("destination_id", destination.ID),
	)

	// ... deliver event to destination

	return nil
}

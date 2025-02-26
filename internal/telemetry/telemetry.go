package telemetry

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"go.uber.org/zap"
)

const (
	batchSize     = 1000
	batchInterval = 5 * time.Second
)

type Telemetry interface {
	Init(ctx context.Context)
	Flush()

	// Events
	ApplicationStarted(ctx context.Context, application ApplicationInfo)
	DestinationCreated(ctx context.Context, destinationType string)
	TenantCreated(ctx context.Context)
}

func New(logger *logging.Logger, enabled bool) Telemetry {
	if !enabled {
		return &NoopTelemetry{}
	}
	return &telemetryImpl{
		logger: logger,
	}
}

type telemetryImpl struct {
	logger    *logging.Logger
	eventChan chan telemetryEvent
	done      chan struct{}
}

func (t *telemetryImpl) sendEvent(event telemetryEvent) {
	select {
	case t.eventChan <- event:
	default:
		t.logger.Warn("telemetry event channel is full, dropping event", zap.Any("event", event))
	}
}

func (t *telemetryImpl) processEvents() {
	ticker := time.NewTicker(batchInterval)
	defer ticker.Stop()

	batch := make([]telemetryEvent, 0)

	for {
		select {
		case event := <-t.eventChan:
			batch = append(batch, event)
			if len(batch) >= batchSize {
				t.sendBatch(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				t.sendBatch(batch)
				batch = batch[:0]
			}
		case <-t.done:
			close(t.eventChan) // Stop accepting new events
			for event := range t.eventChan {
				batch = append(batch, event)
			}
			if len(batch) > 0 {
				t.sendBatch(batch)
			}
			return
		}
	}
}

func (t *telemetryImpl) sendBatch(batch []telemetryEvent) {
	t.logger.Debug("sending telemetry batch", zap.Int("size", len(batch)))
}

func (t *telemetryImpl) Init(ctx context.Context) {
	t.eventChan = make(chan telemetryEvent, batchSize)
	t.done = make(chan struct{})
	go t.processEvents()
}

func (t *telemetryImpl) Flush() {
	close(t.done)
}

func (t *telemetryImpl) ApplicationStarted(ctx context.Context, application ApplicationInfo) {
	event := telemetryEvent{
		EventType: "application_started",
		Payload:   application.ToPayload(),
		Time:      time.Now(),
	}
	t.sendEvent(event)
}

func (t *telemetryImpl) DestinationCreated(ctx context.Context, destinationType string) {
	event := telemetryEvent{
		EventType: "destination_created",
		Payload:   map[string]interface{}{"type": destinationType},
		Time:      time.Now(),
	}
	t.sendEvent(event)
}

func (t *telemetryImpl) TenantCreated(ctx context.Context) {
	event := telemetryEvent{
		EventType: "tenant_created",
		Payload:   map[string]interface{}{},
		Time:      time.Now(),
	}
	t.sendEvent(event)
}

type ApplicationInfo struct {
	Version       string
	MQ            string
	PortalEnabled string
}

func (a *ApplicationInfo) ToPayload() map[string]interface{} {
	return map[string]interface{}{
		"version":        a.Version,
		"mq":             a.MQ,
		"portal_enabled": a.PortalEnabled,
	}
}

type telemetryEvent struct {
	EventType string                 `json:"type"`
	Payload   map[string]interface{} `json:"payload"`
	Time      time.Time              `json:"time"`
}

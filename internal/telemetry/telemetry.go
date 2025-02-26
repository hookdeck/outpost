package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"go.uber.org/zap"
)

type Telemetry interface {
	Init(ctx context.Context)
	Flush()

	// Events
	ApplicationStarted(ctx context.Context, application ApplicationInfo)
	DestinationCreated(ctx context.Context, destinationType string)
	TenantCreated(ctx context.Context)
}

type TelemetryConfig struct {
	Disabled          bool
	BatchSize         int
	BatchInterval     int
	HookdeckSourceURL string
}

func New(logger *logging.Logger, config TelemetryConfig) Telemetry {
	if config.Disabled {
		return &NoopTelemetry{}
	}
	return &telemetryImpl{
		logger: logger,
		config: config,
	}
}

type telemetryImpl struct {
	logger    *logging.Logger
	config    TelemetryConfig
	eventChan chan telemetryEvent
	done      chan struct{}
	client    *http.Client
}

func (t *telemetryImpl) sendEvent(event telemetryEvent) {
	select {
	case t.eventChan <- event:
	default:
		t.logger.Warn("telemetry event channel is full, dropping event", zap.Any("event", event))
	}
}

func (t *telemetryImpl) processEvents() {
	ticker := time.NewTicker(time.Duration(t.config.BatchInterval) * time.Second)
	defer ticker.Stop()

	batch := make([]telemetryEvent, 0)

	for {
		select {
		case event := <-t.eventChan:
			batch = append(batch, event)
			if len(batch) >= t.config.BatchSize {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create worker pool
	maxWorkers := min(len(batch), 10)
	jobs := make(chan telemetryEvent, len(batch))
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for event := range jobs {
				body, err := json.Marshal(event)
				if err != nil {
					t.logger.Debug("failed to marshal event", zap.Error(err))
					continue
				}

				req, err := http.NewRequestWithContext(ctx, "POST", t.config.HookdeckSourceURL, bytes.NewBuffer(body))
				if err != nil {
					t.logger.Debug("failed to create request", zap.Error(err))
					continue
				}
				req.Header.Set("Content-Type", "application/json")

				resp, err := t.client.Do(req)
				if err != nil {
					t.logger.Debug("failed to send event", zap.Error(err))
					continue
				}
				if resp.StatusCode >= 400 {
					t.logger.Debug("failed to send event",
						zap.Int("status", resp.StatusCode),
						zap.Any("event", event))
				}
				resp.Body.Close()
			}
		}()
	}

	// Send jobs to workers
	for _, event := range batch {
		jobs <- event
	}
	close(jobs)

	// Wait for all workers
	wg.Wait()
}

func (t *telemetryImpl) Init(ctx context.Context) {
	t.eventChan = make(chan telemetryEvent, t.config.BatchSize*10)
	t.done = make(chan struct{})
	t.client = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
	}
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

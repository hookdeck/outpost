package api

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/hookdeck/outpost/loadtest/app/internal/config"
	"github.com/hookdeck/outpost/loadtest/app/internal/eventlog"
	"github.com/hookdeck/outpost/loadtest/app/internal/group"
	"github.com/hookdeck/outpost/loadtest/app/internal/metrics"
	"github.com/hookdeck/outpost/loadtest/app/internal/mock"
	"github.com/hookdeck/outpost/loadtest/app/internal/outpost"
	"github.com/hookdeck/outpost/loadtest/app/internal/publisher"
	"github.com/hookdeck/outpost/loadtest/app/internal/run"
)

// App holds all the wired-together components for the load test.
type App struct {
	Config      *config.Config
	Store       *group.Store
	Provisioner *group.Provisioner
	Publisher   *publisher.Publisher
	Tracker     *metrics.InFlightTracker
	MockServer  *mock.Server
	Runs        *run.Controller
}

func NewApp(cfg *config.Config) *App {
	client := outpost.NewClient(cfg.OutpostURL, cfg.APIKey)
	tracker := metrics.NewInFlightTracker(30 * time.Second)

	mockServer := mock.NewServer(nil) // callback set below
	provisioner := group.NewProvisioner(client, mockServer, cfg.MockURL)
	pub := publisher.New(client, tracker)

	app := &App{
		Config:      cfg,
		Store:       group.NewStore(),
		Provisioner: provisioner,
		Publisher:   pub,
		Tracker:     tracker,
		MockServer:  mockServer,
	}
	app.Runs = run.NewController(app.Store, provisioner, pub, tracker, cfg.ExportDir)

	// Wire mock delivery callback → tracker. The tracker owns attribution:
	// it knows which group and phase published the event, and its t0.
	mockServer.SetCallback(func(d mock.DeliveryRecord) {
		if m, _, ok := tracker.RecordDelivery(d.EventID, d.ReceivedAt); !ok {
			// Not in flight and not a recovery — a delivery for an event that
			// already completed, i.e. a duplicate.
			if g, err := app.Store.Get(d.GroupName); err == nil {
				g.Metrics.RecordDuplicate()
				app.emit(d.GroupName, m.Phase).Duplicate()
			}
		}
	})

	// Wire tracker callbacks → group metrics + prometheus + eventlog
	tracker.SetCallbacks(
		func(eventID string, m metrics.EventMeta, e2eLatency time.Duration, allDelivered bool) {
			g, err := app.Store.Get(m.GroupName)
			if err != nil {
				return
			}
			g.Metrics.RecordDelivery(e2eLatency)
			app.emit(m.GroupName, m.Phase).Delivered(e2eLatency, allDelivered)
			deliveredAt := m.ScheduledAt.Add(e2eLatency)
			g.EventLog.Update(eventID, func(r *eventlog.Record) {
				r.Status = eventlog.StatusDelivered
				r.DeliveredAt = &deliveredAt
				r.E2ELatency = e2eLatency.Milliseconds()
			})
		},
		func(eventID string, m metrics.EventMeta) {
			g, err := app.Store.Get(m.GroupName)
			if err != nil {
				return
			}
			g.Metrics.RecordMissing()
			app.emit(m.GroupName, m.Phase).Missing()
			g.EventLog.Update(eventID, func(r *eventlog.Record) {
				r.Status = eventlog.StatusMissing
			})
		},
		func(eventID string, m metrics.EventMeta, e2eLatency time.Duration) {
			g, err := app.Store.Get(m.GroupName)
			if err != nil {
				return
			}
			g.Metrics.RecordRecovered(e2eLatency)
			app.emit(m.GroupName, m.Phase).Recovered(e2eLatency)
			deliveredAt := m.ScheduledAt.Add(e2eLatency)
			g.EventLog.Update(eventID, func(r *eventlog.Record) {
				r.Status = eventlog.StatusRecovered
				r.DeliveredAt = &deliveredAt
				r.E2ELatency = e2eLatency.Milliseconds()
			})
		},
	)

	go app.exportGauges()

	return app
}

func (a *App) emit(profile, phase string) metrics.Emit {
	return metrics.Emit{RunID: a.Publisher.RunID(), Profile: profile, Phase: phase}
}

// exportGauges publishes the sampled state — in-flight depth per profile — that
// no event callback can produce, since it is a level rather than a count.
func (a *App) exportGauges() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		runID := a.Publisher.RunID()
		for _, g := range a.Store.List() {
			metrics.InFlight.With(prometheus.Labels{
				"run_id": runID, "profile": g.Config.Name,
			}).Set(float64(a.Tracker.InFlightCountFor(g.Config.Name)))
		}
	}
}

func (a *App) Stop() {
	a.Tracker.Stop()
}

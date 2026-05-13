package api

import (
	"time"

	"github.com/hookdeck/outpost/loadtest-app/internal/config"
	"github.com/hookdeck/outpost/loadtest-app/internal/eventlog"
	"github.com/hookdeck/outpost/loadtest-app/internal/group"
	"github.com/hookdeck/outpost/loadtest-app/internal/metrics"
	"github.com/hookdeck/outpost/loadtest-app/internal/mock"
	"github.com/hookdeck/outpost/loadtest-app/internal/outpost"
	"github.com/hookdeck/outpost/loadtest-app/internal/publisher"
)

// App holds all the wired-together components for the load test.
type App struct {
	Config      *config.Config
	Store       *group.Store
	Provisioner *group.Provisioner
	Publisher   *publisher.Publisher
	Tracker     *metrics.InFlightTracker
	MockServer  *mock.Server
	EventLog    *eventlog.Log
}

func NewApp(cfg *config.Config) *App {
	client := outpost.NewClient(cfg.OutpostURL, cfg.APIKey)
	tracker := metrics.NewInFlightTracker(30 * time.Second)
	el := eventlog.New(10000)

	mockServer := mock.NewServer(nil) // callback set below
	provisioner := group.NewProvisioner(client, mockServer, cfg.MockURL)
	pub := publisher.New(client, tracker, el)

	app := &App{
		Config:      cfg,
		Store:       group.NewStore(),
		Provisioner: provisioner,
		Publisher:   pub,
		Tracker:     tracker,
		MockServer:  mockServer,
		EventLog:    el,
	}

	// Wire mock delivery callback → tracker + group metrics + eventlog
	mockServer.SetCallback(func(d mock.DeliveryRecord) {
		e2eLatency, _ := tracker.RecordDelivery(d.EventID, d.GroupName, d.ReceivedAt)
		if g, err := app.Store.Get(d.GroupName); err == nil {
			g.Metrics.RecordDelivery(e2eLatency)
		}
		el.Update(d.EventID, func(r *eventlog.Record) {
			r.Status = eventlog.StatusDelivered
			r.DeliveredAt = &d.ReceivedAt
			r.E2ELatency = e2eLatency.Milliseconds()
		})
	})

	// Wire tracker callbacks → group metrics + eventlog
	tracker.SetCallbacks(
		nil,
		func(eventID, groupName string) {
			if g, err := app.Store.Get(groupName); err == nil {
				g.Metrics.RecordMissing()
			}
			el.Update(eventID, func(r *eventlog.Record) {
				r.Status = eventlog.StatusMissing
			})
		},
		func(eventID, groupName string) {
			if g, err := app.Store.Get(groupName); err == nil {
				g.Metrics.RecordRecovered()
			}
			el.Update(eventID, func(r *eventlog.Record) {
				r.Status = eventlog.StatusRecovered
			})
		},
	)

	return app
}

func (a *App) Stop() {
	a.Tracker.Stop()
}

package api

import (
	"time"

	"github.com/hookdeck/outpost/loadtest/app/internal/config"
	"github.com/hookdeck/outpost/loadtest/app/internal/eventlog"
	"github.com/hookdeck/outpost/loadtest/app/internal/group"
	"github.com/hookdeck/outpost/loadtest/app/internal/metrics"
	"github.com/hookdeck/outpost/loadtest/app/internal/mock"
	"github.com/hookdeck/outpost/loadtest/app/internal/outpost"
	"github.com/hookdeck/outpost/loadtest/app/internal/publisher"
)

// App holds all the wired-together components for the load test.
type App struct {
	Config      *config.Config
	Store       *group.Store
	Provisioner *group.Provisioner
	Publisher   *publisher.Publisher
	Tracker     *metrics.InFlightTracker
	MockServer  *mock.Server
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

	// Wire mock delivery callback → tracker + group metrics + eventlog
	mockServer.SetCallback(func(d mock.DeliveryRecord) {
		e2eLatency, _ := tracker.RecordDelivery(d.EventID, d.GroupName, d.ReceivedAt)
		g, err := app.Store.Get(d.GroupName)
		if err != nil {
			return
		}
		g.Metrics.RecordDelivery(e2eLatency)
		g.EventLog.Update(d.EventID, func(r *eventlog.Record) {
			r.Status = eventlog.StatusDelivered
			r.DeliveredAt = &d.ReceivedAt
			// Compute e2e from record timestamps to avoid race with tracker.RecordPublish
			if !r.PublishedAt.IsZero() {
				r.E2ELatency = d.ReceivedAt.Sub(r.PublishedAt).Milliseconds()
			} else {
				r.E2ELatency = e2eLatency.Milliseconds()
			}
		})
	})

	// Wire tracker callbacks → group metrics + eventlog
	tracker.SetCallbacks(
		nil,
		func(eventID, groupName string) {
			g, err := app.Store.Get(groupName)
			if err != nil {
				return
			}
			g.Metrics.RecordMissing()
			g.EventLog.Update(eventID, func(r *eventlog.Record) {
				r.Status = eventlog.StatusMissing
			})
		},
		func(eventID, groupName string) {
			g, err := app.Store.Get(groupName)
			if err != nil {
				return
			}
			g.Metrics.RecordRecovered()
			g.EventLog.Update(eventID, func(r *eventlog.Record) {
				r.Status = eventlog.StatusRecovered
			})
		},
	)

	return app
}

func (a *App) Stop() {
	a.Tracker.Stop()
}

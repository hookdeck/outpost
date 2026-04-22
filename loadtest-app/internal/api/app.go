package api

import (
	"time"

	"github.com/hookdeck/outpost/loadtest-app/internal/config"
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

	// Wire mock delivery callback → tracker + group metrics
	mockServer.SetCallback(func(d mock.DeliveryRecord) {
		e2eLatency, _ := tracker.RecordDelivery(d.EventID, d.GroupName, d.ReceivedAt)
		if g, err := app.Store.Get(d.GroupName); err == nil {
			g.Metrics.RecordDelivery(e2eLatency)
		}
	})

	// Wire tracker callbacks → group metrics
	tracker.SetCallbacks(
		nil,
		func(eventID, groupName string) {
			if g, err := app.Store.Get(groupName); err == nil {
				g.Metrics.RecordMissing()
			}
		},
		func(eventID, groupName string) {
			if g, err := app.Store.Get(groupName); err == nil {
				g.Metrics.RecordRecovered()
			}
		},
	)

	return app
}

func (a *App) Stop() {
	a.Tracker.Stop()
}

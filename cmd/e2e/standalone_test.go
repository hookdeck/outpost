package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/require"
)

// standaloneApp is an Outpost run by a test, outside basicSuite, so a test
// can run several in turn against the same Redis.
type standaloneApp struct {
	t      *testing.T
	cfg    config.Config
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
}

// prepareStandaloneConfig validates cfg and applies its migrations.
func prepareStandaloneConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	require.NoError(t, cfg.Validate(config.Flags{}))
	configs.ApplyMigrations(t, cfg)
}

// startStandaloneApp boots Outpost and waits until it is healthy.
func startStandaloneApp(t *testing.T, cfg config.Config) *standaloneApp {
	t.Helper()
	prepareStandaloneConfig(t, &cfg)
	ctx, cancel := context.WithCancel(context.Background())
	a := &standaloneApp{t: t, cfg: cfg, cancel: cancel, done: make(chan error, 1)}
	go func() { a.done <- app.New(&a.cfg).Run(ctx) }()
	t.Cleanup(a.stop)

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-a.done:
			t.Fatalf("outpost exited during startup: %v", err)
		default:
		}
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/healthz", cfg.APIPort))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return a
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("outpost did not become healthy on port %d", cfg.APIPort)
	return nil
}

// stop shuts Outpost down and waits for it.
func (a *standaloneApp) stop() {
	a.once.Do(func() {
		a.cancel()
		select {
		case <-a.done:
		case <-time.After(30 * time.Second):
			a.t.Log("outpost did not shut down within 30s")
		}
	})
}

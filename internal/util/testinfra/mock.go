package testinfra

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/destinationmockserver"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

var mockServerOnce sync.Once

func GetMockServer(t *testing.T) string {
	cfg := ReadConfig()
	if cfg.MockServerURL == "" {
		mockServerOnce.Do(func() {
			startMockServer(cfg)
		})
	}
	return cfg.MockServerURL
}

func newMockServerConfig() destinationmockserver.DestinationMockServerConfig {
	return destinationmockserver.DestinationMockServerConfig{
		Port: testutil.RandomPortNumber(),
	}
}

func startMockServer(cfg *Config) {
	mockServerConfig := newMockServerConfig()
	cfg.MockServerURL = fmt.Sprintf("http://localhost:%d", mockServerConfig.Port)
	go func() {
		mockServer := destinationmockserver.New(mockServerConfig)
		if err := mockServer.Run(context.Background()); err != nil {
			panic(err)
		}
	}()
}

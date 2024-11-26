package main

import (
	"context"

	"github.com/hookdeck/outpost/internal/destinationmockserver"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	mockServer := destinationmockserver.New(destinationmockserver.DestinationMockServerConfig{
		Port: 5555,
	})
	if err := mockServer.Run(context.Background()); err != nil {
		return err
	}
	return nil
}

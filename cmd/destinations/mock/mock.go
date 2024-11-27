package main

import (
	"context"
	"os"
	"strconv"

	"github.com/hookdeck/outpost/internal/destinationmockserver"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	mockServer := destinationmockserver.New(destinationmockserver.DestinationMockServerConfig{
		Port: getPort(),
	})
	if err := mockServer.Run(context.Background()); err != nil {
		return err
	}
	return nil
}

const defaultPort = 5555

func getPort() int {
	portEnv := os.Getenv("PORT")
	if portEnv == "" {
		return defaultPort
	}
	if port, err := strconv.Atoi(portEnv); err == nil {
		return port
	}
	return defaultPort
}

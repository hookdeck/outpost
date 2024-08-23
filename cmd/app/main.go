package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/otel"
	"github.com/hookdeck/EventKit/internal/services/api"
	"github.com/hookdeck/EventKit/internal/services/data"
	"github.com/hookdeck/EventKit/internal/services/delivery"
)

type Service interface {
	Run(ctx context.Context) error
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	flags := config.ParseFlags()
	if err := config.Parse(flags); err != nil {
		return err
	}

	// Set up cancellation context and waitgroup
	ctx, cancel := context.WithCancel(context.Background())

	// Set up OpenTelemetry.
	if config.OpenTelemetry != nil {
		otelShutdown, err := otel.SetupOTelSDK(ctx)
		if err != nil {
			return err
		}
		// Handle shutdown properly so nothing leaks.
		defer func() {
			err = errors.Join(err, otelShutdown(context.Background()))
		}()
	}

	// Initialize waitgroup
	wg := &sync.WaitGroup{}

	// Construct services based on config
	services := []Service{}
	switch config.Service {
	case config.ServiceTypeAPI:
		services = append(services, api.NewService(ctx, wg))
	case config.ServiceTypeData:
		services = append(services, data.NewService(ctx, wg))
	case config.ServiceTypeDelivery:
		services = append(services, delivery.NewService(ctx, wg))
	case config.ServiceTypeSingular:
		services = append(services,
			api.NewService(ctx, wg),
			data.NewService(ctx, wg),
			delivery.NewService(ctx, wg),
		)
	default:
		return errors.New(fmt.Sprintf("unknown service: %s", flags.Service))
	}

	// Initialize how many services the waitgroup should expect.
	// Once all services are done, we can exit.
	// Each service will wait for the context to be cancelled before shutting down.
	wg.Add(len(services))

	// Start services
	for _, service := range services {
		go service.Run(ctx)
	}

	// Handle sigterm and await termChan signal
	termChan := make(chan os.Signal)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	<-termChan // Blocks here until interrupted

	// Handle shutdown
	fmt.Println("*********************************\nShutdown signal received\n*********************************")
	cancel()  // Signal cancellation to context.Context
	wg.Wait() // Block here until all workers are done

	return nil
}

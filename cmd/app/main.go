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
	"github.com/hookdeck/EventKit/internal/redis"
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

func run(mainContext context.Context) error {
	flags := config.ParseFlags()
	if err := config.Parse(flags); err != nil {
		return err
	}

	// Set up cancellation context and waitgroup
	ctx, cancel := context.WithCancel(mainContext)

	// Set up OpenTelemetry.
	if config.OpenTelemetry != nil {
		otelShutdown, err := otel.SetupOTelSDK(ctx)
		if err != nil {
			cancel()
			return err
		}
		// Handle shutdown properly so nothing leaks.
		defer func() {
			err = errors.Join(err, otelShutdown(context.Background()))
		}()

		// QUESTION: what if a service doesn't need Redis? Is it unnecessary to initalize the client here?
		if err := redis.InstrumentOpenTelemetry(); err != nil {
			cancel()
			return err
		}
	}

	// Initialize waitgroup
	// Once all services are done, we can exit.
	// Each service will wait for the context to be cancelled before shutting down.
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
		cancel()
		return fmt.Errorf("unknown service: %s", flags.Service)
	}

	// Start services
	for _, service := range services {
		go service.Run(ctx)
	}

	// Handle sigterm and await termChan signal
	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	<-termChan // Blocks here until interrupted

	// Handle shutdown
	fmt.Println("*********************************\nShutdown signal received\n*********************************")
	cancel()  // Signal cancellation to context.Context
	wg.Wait() // Block here until all workers are done

	return nil
}

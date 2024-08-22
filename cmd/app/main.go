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
	"github.com/hookdeck/EventKit/internal/flag"
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
	flags := flag.Parse()
	if err := config.Parse(flags.Config); err != nil {
		return err
	}

	// Set up cancellation context and waitgroup
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	services := []Service{}

	switch flags.Service {
	case "api":
		services = append(services, api.NewService(ctx, wg))
	case "data":
		services = append(services, data.NewService(ctx, wg))
	case "delivery":
		services = append(services, delivery.NewService(ctx, wg))
	case "":
		services = append(services,
			api.NewService(ctx, wg),
			data.NewService(ctx, wg),
			delivery.NewService(ctx, wg),
		)
	default:
		return errors.New(fmt.Sprintf("unknown service: %s", flags.Service))
	}

	// Register services with waitgroup.
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

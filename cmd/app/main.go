package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/flag"
	"github.com/hookdeck/EventKit/internal/services/api"
	"github.com/hookdeck/EventKit/internal/services/data"
	"github.com/hookdeck/EventKit/internal/services/delivery"
	"golang.org/x/sync/errgroup"
)

type Service interface {
	Run(ctx context.Context) error
}

func run(ctx context.Context) error {
	flags := flag.Parse()
	if err := config.Parse(flags.Config); err != nil {
		return err
	}

	switch flags.Service {

	case "api":
		return api.Run(ctx)

	case "delivery":
		return delivery.Run(ctx)

	case "data":
		return data.Run(ctx)

	case "":
		// Run all services.
		// TODO: Investigate how goroutine affect graceful shutdown, fatal, restart, etc.
		// @see https://github.com/gin-gonic/gin/issues/346

		var g errgroup.Group
		g.Go(func() error {
			return api.Run(ctx)
		})
		g.Go(func() error {
			return delivery.Run(ctx)
		})
		g.Go(func() error {
			return data.Run(ctx)
		})

		err := g.Wait()
		return err
	default:
		return errors.New(fmt.Sprintf("unknown service: %s", flags.Service))
	}
}

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

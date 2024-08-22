package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/hookdeck/EventKit/internal/services/api"
	"github.com/hookdeck/EventKit/internal/services/data"
	"github.com/hookdeck/EventKit/internal/services/delivery"
	"golang.org/x/sync/errgroup"
)

type Service interface {
	Run(ctx context.Context) error
}

func run(ctx context.Context, w io.Writer, args []string) error {
	serviceName := ""
	if len(args) > 1 {
		serviceName = args[1]
	}

	if serviceName == "" {
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
	} else if serviceName == "api" {
		return api.Run(ctx)
	} else if serviceName == "delivery" {
		return delivery.Run(ctx)
	} else if serviceName == "data" {
		return data.Run(ctx)
	} else {
		return errors.New(fmt.Sprintf("unknown service: %s", serviceName))
	}
}

func main() {
	ctx := context.Background()
	if err := run(ctx, os.Stdout, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

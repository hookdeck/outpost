package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
)

func main() {
	flags := config.ParseFlags()
	cfg, err := config.Parse(flags)
	if err != nil {
		handleErr(err)
		return
	}
	application := app.New(cfg)
	ctx := context.Background()
	if err := application.Run(ctx); err != nil {
		handleErr(err)
		return
	}
}

func handleErr(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(1)
}

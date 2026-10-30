package main

import (
	"context"
	"embed"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"
)

//go:embed samples/events.json
var samplesFS embed.FS

// sampleEventsJSON is the sample catalog, also what make up/mcp loads.
func sampleEventsJSON() []byte {
	b, _ := samplesFS.ReadFile("samples/events.json")
	return b
}

func samplesCommand() *cli.Command {
	return &cli.Command{
		Name:  "samples",
		Usage: "Write the sample events/list result, for tools that import one",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "out", Required: true, Usage: "directory to write to (created if missing)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			dir := cmd.String("out")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			p := filepath.Join(dir, "events.json")
			if err := os.WriteFile(p, sampleEventsJSON(), 0o644); err != nil {
				return err
			}
			fmt.Println(p)
			return nil
		},
	}
}

// samplesHandler serves the sample events/list result. It is a dev
// convenience next to the MCP endpoint, not part of it.
func samplesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(sampleEventsJSON())
	})
}

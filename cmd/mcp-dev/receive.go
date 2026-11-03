package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
	"github.com/urfave/cli/v3"
)

func receiveCommand() *cli.Command {
	return &cli.Command{
		Name:  "receive",
		Usage: "Run a webhook receiver that verifies MCP Events deliveries",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "listen", Value: ":4501", Usage: "listen address (all interfaces so Docker can reach it)"},
			&cli.StringFlag{Name: "public-url", Value: "http://host.docker.internal:4501", Usage: "base URL the server reaches this receiver at"},
			&cli.StringFlag{Name: "path", Value: "/hook", Usage: "callback path"},
			&cli.StringSliceFlag{Name: "secret", Sources: cli.EnvVars("MCP_SECRET"), Usage: "signing secret, repeatable (generated when absent)"},
			&cli.IntFlag{Name: "event-status", Value: 200, Usage: "HTTP status for event deliveries, e.g. 500 to watch retries"},
			&cli.StringFlag{Name: "profile", Value: "spec", Usage: "rules to check: spec or outpost"},
			&cli.BoolFlag{Name: "json", Usage: "print one JSON line per request"},
		},
		Action: receive,
	}
}

func parseProfile(s string) (mcpreceiver.Profile, error) {
	switch s {
	case "spec":
		return mcpreceiver.ProfileSpec, nil
	case "outpost":
		return mcpreceiver.ProfileOutpost, nil
	}
	return 0, usagef("--profile must be spec or outpost: %q", s)
}

func receive(ctx context.Context, cmd *cli.Command) error {
	profile, err := parseProfile(cmd.String("profile"))
	if err != nil {
		return err
	}
	secrets := cmd.StringSlice("secret")
	generated := len(secrets) == 0
	if generated {
		secrets = []string{mcpclient.NewSecret()}
	}
	cfg := mcpreceiver.PathConfig{Secrets: secrets, RejectUnverified: true}
	if n := cmd.Int("event-status"); n != 200 {
		cfg.Event = mcpreceiver.Status(n)
	}

	var mu sync.Mutex
	reported := map[mcpreceiver.Violation]bool{}
	var live *mcpreceiver.Receiver
	asJSON := cmd.Bool("json")
	rcv, err := mcpreceiver.Listen(mcpreceiver.Options{
		Addr:      cmd.String("listen"),
		PublicURL: cmd.String("public-url"),
		Profile:   profile,
		OnRequest: func(q mcpreceiver.Request) {
			mu.Lock()
			defer mu.Unlock()
			if asJSON {
				line, _ := json.Marshal(map[string]any{"time": q.Received, "path": q.Path, "kind": q.Kind(), "headers": q.Header, "body": json.RawMessage(q.Body)})
				fmt.Println(string(line))
			} else {
				fmt.Printf("%s %s\n", stamp(), describe(q, secrets))
			}
			if live == nil {
				return
			}
			for _, v := range live.Violations() {
				if !reported[v] {
					reported[v] = true
					fmt.Printf("VIOLATION %s: %s\n", v.Check, v.Detail)
				}
			}
		},
	})
	if err != nil {
		return err
	}
	defer rcv.Close()
	rcv.Set(cmd.String("path"), cfg)
	mu.Lock()
	live = rcv
	mu.Unlock()
	fmt.Printf("callback URL: %s%s\n", rcv.BaseURL(), cmd.String("path"))
	if generated {
		fmt.Println("secret:", secrets[0])
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	violations := rcv.Violations()
	fmt.Fprintf(os.Stderr, "\n%d requests, %d violations\n", len(rcv.AllRequests()), len(violations))
	if len(violations) > 0 {
		return exitError{1}
	}
	return nil
}

package main

import (
	"context"
	"fmt"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpcatalog"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/urfave/cli/v3"
)

func diffCommand() *cli.Command {
	return &cli.Command{
		Name:      "diff",
		Usage:     "Compare an event list file with another file or a server's events/list",
		ArgsUsage: "FILE [FILE2]",
		Description: `Compares events by name, ignoring order. Exit status 0 means equal, 1 differences,
2 a usage or load error. The delivery field is ignored unless --ignore is given.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "server", Sources: cli.EnvVars("MCP_SERVER_URL"), Usage: "compare with this server's events/list instead of FILE2"},
			&cli.StringFlag{Name: "principal", Sources: cli.EnvVars("MCP_PRINCIPAL"), Usage: "bearer token to send to --server (default: random)"},
			&cli.StringSliceFlag{Name: "ignore", Value: mcpcatalog.DefaultIgnore, Usage: "top-level event field to ignore, repeatable"},
		},
		Action: diffAction,
	}
}

func diffAction(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	server := cmd.String("server")
	if !(len(args) == 2 && server == "") && !(len(args) == 1 && server != "") {
		return usagef("want FILE and one of FILE2 or --server")
	}
	a, err := mcpcatalog.LoadFile(args[0])
	if err != nil {
		return usageError{err}
	}
	var b []mcpcatalog.Event
	if len(args) == 2 {
		b, err = mcpcatalog.LoadFile(args[1])
		if err != nil {
			return usageError{err}
		}
	} else {
		if b, err = catalogFromServer(ctx, server, cmd.String("principal")); err != nil {
			return usageError{err}
		}
	}
	changes, err := mcpcatalog.Diff(a, b, cmd.StringSlice("ignore"))
	if err != nil {
		return usageError{err}
	}
	if len(changes) == 0 {
		fmt.Printf("equal: %d events\n", len(a))
		return nil
	}
	lastEvent := ""
	for _, ch := range changes {
		switch ch.Kind {
		case "added":
			fmt.Printf("+ %s (only in B)\n", ch.Event)
		case "removed":
			fmt.Printf("- %s (only in A)\n", ch.Event)
		default:
			if ch.Event != lastEvent {
				fmt.Printf("~ %s\n", ch.Event)
			}
			fmt.Printf("    %s: %s -> %s\n", ch.Path, compact(ch.From, 40), compact(ch.To, 40))
		}
		lastEvent = ch.Event
	}
	return exitError{1}
}

// catalogFromServer lists a server's events; without a principal it uses a
// random one.
func catalogFromServer(ctx context.Context, server, principal string) ([]mcpcatalog.Event, error) {
	if principal == "" {
		principal = randomPrincipal()
	}
	c := mcpclient.New(server, principal, mcpclient.Stateless(), mcpclient.WithClientInfoName("outpost-mcp-dev"))
	return mcpcatalog.FromServer(ctx, c)
}

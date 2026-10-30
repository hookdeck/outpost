package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpcatalog"
	"github.com/urfave/cli/v3"
)

func publishCommand() *cli.Command {
	return &cli.Command{
		Name:  "publish",
		Usage: "Publish sample events to a tenant, built from each event's payload schema",
		Description: `Outpost keeps an event only when it is delivered to a matching destination, so
events published to a tenant without one are accepted but never listed. Subscribe
first, or pass --sink to create a temporary webhook destination for the run.

The events come from the embedded sample, --file (an events/list result) or
--server (a server's events/list).`,
		Flags: append([]cli.Flag{
			&cli.StringFlag{Name: "tenant", Value: "demo", Usage: "tenant to publish to"},
			&cli.StringFlag{Name: "file", Usage: "events/list result to read the events from"},
			&cli.StringFlag{Name: "server", Sources: cli.EnvVars("MCP_SERVER_URL"), Usage: "MCP Events server to read the events from"},
			&cli.StringFlag{Name: "principal", Sources: cli.EnvVars("MCP_PRINCIPAL"), Usage: "bearer token to send to --server (default: random)"},
			&cli.StringSliceFlag{Name: "event", Usage: "publish only this event, repeatable"},
			&cli.IntFlag{Name: "count", Value: 3, Usage: "events to publish per event name (1-1000)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "print the data without publishing"},
			&cli.BoolFlag{Name: "sink", Usage: "create a temporary webhook destination that accepts everything, and delete it afterwards"},
			&cli.StringFlag{Name: "sink-listen", Value: ":4502", Usage: "address the sink listens on"},
			&cli.StringFlag{Name: "sink-url", Value: "http://host.docker.internal:4502", Usage: "URL Outpost uses to reach the sink"},
		}, outpostFlags()...),
		Action: publishAction,
	}
}

func publishAction(ctx context.Context, cmd *cli.Command) (retErr error) {
	c, err := outpostConn(cmd)
	if err != nil {
		return err
	}
	count := cmd.Int("count")
	if count < 1 || count > 1000 {
		return usagef("--count %d: want 1 to 1000", count)
	}
	if cmd.String("file") != "" && cmd.String("server") != "" {
		return usagef("pass --file or --server, not both")
	}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	events, err := publishSource(ctx, cmd)
	if err != nil {
		return err
	}
	events, err = selectEvents(events, cmd.StringSlice("event"))
	if err != nil {
		return err
	}
	dry := cmd.Bool("dry-run")
	tenant := cmd.String("tenant")
	var sink *webhookSink
	if cmd.Bool("sink") && !dry {
		names := make([]string, len(events))
		for i, e := range events {
			names[i], _ = e["name"].(string)
		}
		sink, err = startSink(ctx, c, tenant, cmd.String("sink-listen"), cmd.String("sink-url"), names)
		if err != nil {
			return err
		}
		fmt.Printf("sink destination %s (%s)\n", sink.id, cmd.String("sink-url"))
		defer func() {
			if err := sink.close(); err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				if retErr == nil {
					retErr = exitError{1}
				}
			} else {
				fmt.Printf("deleted sink destination %s\n", sink.id)
			}
		}()
	}
	now := time.Now()
	failed := false
	var published int64
	for _, e := range events {
		name, _ := e["name"].(string)
		schema, _ := e["payloadSchema"].(map[string]any)
		if schema == nil {
			fmt.Printf("%s  skipped: no payloadSchema\n", name)
			continue
		}
		var ids []string
		matched := 0
		var errs []string
		for i := 0; i < count; i++ {
			data := mcpcatalog.Sample(schema, i, now)
			if dry {
				fmt.Printf("%s  %s\n", name, compact(data, 60))
				continue
			}
			res, err := c.publishOne(ctx, tenant, name, data)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			ids = append(ids, res.ID)
			published++
			if len(res.DestinationIDs) > matched {
				matched = len(res.DestinationIDs)
			}
		}
		if dry {
			continue
		}
		if len(errs) > 0 {
			failed = true
			fmt.Printf("%s  %d of %d failed: %s\n", name, len(errs), count, errs[0])
		}
		if len(ids) == 0 {
			continue
		}
		fmt.Printf("%s  %d published  destinations: %d  (%s)\n", name, len(ids), matched, strings.Join(ids, ", "))
		if matched == 0 {
			fmt.Printf("warning: no destination matched %s; Outpost keeps an event only when it is delivered (subscribe first, or pass --sink)\n", name)
		}
	}
	if sink != nil {
		sink.waitFor(ctx, published, sinkWait)
		fmt.Printf("sink received %d of %d deliveries\n", sink.received.Load(), published)
	}
	if failed {
		return exitError{1}
	}
	return nil
}

func publishSource(ctx context.Context, cmd *cli.Command) ([]mcpcatalog.Event, error) {
	switch {
	case cmd.String("file") != "":
		return mcpcatalog.LoadFile(cmd.String("file"))
	case cmd.String("server") != "":
		return catalogFromServer(ctx, cmd.String("server"), cmd.String("principal"))
	}
	return mcpcatalog.Decode(sampleEventsJSON())
}

func selectEvents(events []mcpcatalog.Event, names []string) ([]mcpcatalog.Event, error) {
	if len(names) == 0 {
		return events, nil
	}
	var out []mcpcatalog.Event
	for _, want := range names {
		found := false
		for _, e := range events {
			if e["name"] == want {
				out = append(out, e)
				found = true
				break
			}
		}
		if !found {
			return nil, usagef("--event %q: not in the event list", want)
		}
	}
	return out, nil
}

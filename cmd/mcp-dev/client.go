package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpcatalog"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/urfave/cli/v3"
)

func clientFlags(extra ...cli.Flag) []cli.Flag {
	return append([]cli.Flag{
		&cli.StringFlag{Name: "server", Value: "http://127.0.0.1:4500", Sources: cli.EnvVars("MCP_SERVER_URL"), Usage: "MCP Events server URL"},
		&cli.StringFlag{Name: "principal", Sources: cli.EnvVars("MCP_PRINCIPAL"), Usage: "bearer token to send"},
		&cli.BoolFlag{Name: "json", Usage: "print the raw JSON-RPC response"},
	}, extra...)
}

func subscribeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "event", Required: true, Usage: "event name"},
		&cli.StringFlag{Name: "url", Required: true, Usage: "callback URL"},
		&cli.StringFlag{Name: "secret", Sources: cli.EnvVars("MCP_SECRET"), Usage: "whsec_ signing secret"},
		&cli.StringFlag{Name: "arguments", Usage: "subscription arguments, a JSON object"},
		&cli.StringFlag{Name: "ttl-ms", Usage: "suggested lifetime in ms, or null"},
		&cli.StringFlag{Name: "cursor", Usage: "resume cursor"},
		&cli.Int64Flag{Name: "max-age-ms", Usage: "replay window in ms"},
	}
}

func clientCommand() *cli.Command {
	return &cli.Command{
		Name:  "client",
		Usage: "Call an MCP Events server",
		Commands: []*cli.Command{
			{Name: "discover", Usage: "server/discover", Flags: clientFlags(), Action: runClient(discover)},
			{Name: "list", Usage: "events/list", Flags: clientFlags(
				&cli.StringFlag{Name: "cursor", Usage: "page cursor"},
				&cli.BoolFlag{Name: "all", Usage: "follow nextCursor and print every event"},
			), Action: runClient(list)},
			{Name: "subscribe", Usage: "events/subscribe (a generated secret is printed)", Flags: clientFlags(subscribeFlags()...), Action: runClient(subscribe)},
			{Name: "refresh", Usage: "events/subscribe again with the same key; prints deliveryStatus", Flags: clientFlags(subscribeFlags()...), Action: runClient(subscribe)},
			{Name: "unsubscribe", Usage: "events/unsubscribe", Flags: clientFlags(
				&cli.StringFlag{Name: "event", Required: true, Usage: "event name"},
				&cli.StringFlag{Name: "url", Required: true, Usage: "callback URL"},
				&cli.StringFlag{Name: "arguments", Usage: "subscription arguments, a JSON object"},
			), Action: runClient(unsubscribe)},
		},
	}
}

func runClient(f func(context.Context, *cli.Command, *mcpclient.Client) error) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		c := mcpclient.New(cmd.String("server"), cmd.String("principal"), mcpclient.Stateless(), mcpclient.WithClientInfoName("outpost-mcp-dev"))
		return f(ctx, cmd, c)
	}
}

func parseObject(flag, s string) (map[string]any, error) {
	if s == "" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil || m == nil {
		return nil, usagef("--%s must be a JSON object: %q", flag, s)
	}
	return m, nil
}

// show prints a response: the result, or the error on stderr with exit
// status 1.
func show(cmd *cli.Command, resp *mcpclient.Response) error {
	if cmd.Bool("json") {
		fmt.Println(string(resp.Raw))
	}
	switch {
	case resp.Error != nil:
		fmt.Fprintf(os.Stderr, "error %d %s %s\n", resp.Error.Code, resp.Error.Message, resp.Error.Data)
		return exitError{1}
	case resp.Status < 200 || resp.Status > 299:
		fmt.Fprintf(os.Stderr, "http %d: %s\n", resp.Status, clip(string(resp.Raw), 500))
		return exitError{1}
	}
	if !cmd.Bool("json") {
		var out json.RawMessage = resp.Result
		pretty, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			pretty = out
		}
		fmt.Println(string(pretty))
	}
	return nil
}

func fail(err error) error {
	fmt.Fprintln(os.Stderr, "error:", err)
	return exitError{1}
}

func discover(ctx context.Context, cmd *cli.Command, c *mcpclient.Client) error {
	_, resp, err := c.Discover(ctx)
	if resp == nil {
		return fail(err)
	}
	return show(cmd, resp)
}

func list(ctx context.Context, cmd *cli.Command, c *mcpclient.Client) error {
	if !cmd.Bool("all") {
		cursor := cmd.String("cursor")
		resp, err := c.EventsListPage(ctx, cursor)
		if err != nil {
			return fail(err)
		}
		return show(cmd, resp)
	}
	all, err := mcpcatalog.FromServer(ctx, c)
	if err != nil {
		return fail(err)
	}
	out, _ := json.MarshalIndent(map[string]any{"resultType": "complete", "events": all}, "", "  ")
	fmt.Println(string(out))
	return nil
}

func parseTTL(s string) (any, error) {
	switch s {
	case "":
		return nil, nil
	case "null":
		return mcpclient.TTLNull, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, usagef("--ttl-ms must be a number of milliseconds or null: %q", s)
	}
	return n, nil
}

func subscribe(ctx context.Context, cmd *cli.Command, c *mcpclient.Client) error {
	args, err := parseObject("arguments", cmd.String("arguments"))
	if err != nil {
		return err
	}
	ttl, err := parseTTL(cmd.String("ttl-ms"))
	if err != nil {
		return err
	}
	secret := cmd.String("secret")
	if secret == "" {
		if cmd.Name == "refresh" {
			return usagef("refresh needs --secret (the one the subscription uses)")
		}
		secret = mcpclient.NewSecret()
		if cmd.Bool("json") {
			fmt.Fprintln(os.Stderr, "secret:", secret)
		} else {
			fmt.Println("secret:", secret)
		}
	}
	p := mcpclient.SubscribeParams{Name: cmd.String("event"), Arguments: args, URL: cmd.String("url"), Secret: secret, TTLMs: ttl}
	if s := cmd.String("cursor"); s != "" {
		p.Cursor = s
	}
	if cmd.IsSet("max-age-ms") {
		p.MaxAgeMs = cmd.Int64("max-age-ms")
	}
	resp, err := c.Subscribe(ctx, p)
	if err != nil {
		return fail(err)
	}
	if err := show(cmd, resp); err != nil {
		return err
	}
	var res mcpclient.SubscribeResult
	if !cmd.Bool("json") && json.Unmarshal(resp.Result, &res) == nil && res.RefreshBefore != nil {
		if t, err := time.Parse(time.RFC3339Nano, *res.RefreshBefore); err == nil {
			fmt.Printf("refresh before: %s (in %s)\n", *res.RefreshBefore, time.Until(t).Round(time.Second))
		}
	}
	return nil
}

func unsubscribe(ctx context.Context, cmd *cli.Command, c *mcpclient.Client) error {
	args, err := parseObject("arguments", cmd.String("arguments"))
	if err != nil {
		return err
	}
	resp, err := c.Unsubscribe(ctx, mcpclient.UnsubscribeParams{Name: cmd.String("event"), Arguments: args, URL: cmd.String("url")})
	if err != nil {
		return fail(err)
	}
	return show(cmd, resp)
}

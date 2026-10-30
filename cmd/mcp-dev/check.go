package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpcheck"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
	"github.com/urfave/cli/v3"
)

const checkDescription = `Runs the SEP-3415 webhook flow against a server and reports one line per rule.

With --publish-tenant the check publishes events to Outpost itself; without it
the check waits for an event you trigger upstream (wait mode), and the checks
that need to control what is published are skipped.

Publish mode assumes Outpost's argument semantics: each subscription argument
filters on the data field of the same name. The spec leaves argument meaning
to the server.

Not covered: webhook mode only (no poll, stream, listChanged), verification
without a challenge (allowlist, out-of-band, well-known document), cursor
replay, maxAgeMs, truncated, gap and terminated bodies (shape-checked when
seen), v1a signatures, SSRF and redirect rules, ResourceExhausted, suspension,
throttling, and validation of delivered data against payloadSchema.

Exit status: 0 no FAIL, 1 a FAIL, 2 usage.`

func checkCommand() *cli.Command {
	return &cli.Command{
		Name:        "check",
		Usage:       "Run the conformance checks against an MCP Events server",
		Description: checkDescription,
		Flags: append([]cli.Flag{
			&cli.StringFlag{Name: "server", Value: "http://127.0.0.1:4500", Sources: cli.EnvVars("MCP_SERVER_URL"), Usage: "MCP Events server URL"},
			&cli.StringFlag{Name: "principal", Usage: "bearer token (default: random per run; verification is rate limited per principal)"},
			&cli.StringFlag{Name: "second-principal", Usage: "other bearer token for the isolation check (default: principal + -2)"},
			&cli.StringFlag{Name: "event", Usage: "event to subscribe to (default: the first webhook event with a scalar argument)"},
			&cli.StringFlag{Name: "arguments", Usage: "subscription arguments, a JSON object"},
			&cli.StringFlag{Name: "data", Usage: "payload that matches the arguments, a JSON object"},
			&cli.StringFlag{Name: "other-data", Usage: "payload that doesn't match, a JSON object"},
			&cli.IntFlag{Name: "ttl-ms", Value: 600000, Usage: "suggested subscription lifetime in ms"},
			&cli.StringFlag{Name: "listen", Value: ":4501", Usage: "receiver listen address (all interfaces so Docker can reach it)"},
			&cli.StringFlag{Name: "public-url", Value: "http://host.docker.internal:4501", Usage: "base URL the server reaches the receiver at"},
			&cli.DurationFlag{Name: "wait", Value: 30 * time.Second, Usage: "wait per expected delivery"},
			&cli.StringFlag{Name: "profile", Value: "spec", Usage: "rules to check: spec or outpost"},
			&cli.StringFlag{Name: "publish-tenant", Usage: "publish to this Outpost tenant (turns publish mode on)"},
			&cli.BoolFlag{Name: "json", Usage: "print the report as JSON"},
		}, outpostFlags()...),
		Action: check,
	}
}

func randomPrincipal() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "mcp-check-" + hex.EncodeToString(b)
}

func check(ctx context.Context, cmd *cli.Command) error {
	profile, err := parseProfile(cmd.String("profile"))
	if err != nil {
		return err
	}
	if cmd.Int("ttl-ms") < 1 {
		return usagef("--ttl-ms must be positive: %d", cmd.Int("ttl-ms"))
	}
	var objs [3]map[string]any
	for i, f := range []string{"arguments", "data", "other-data"} {
		if objs[i], err = parseObject(f, cmd.String(f)); err != nil {
			return err
		}
	}
	c, err := outpostConn(cmd)
	if err != nil {
		return err
	}
	principal := cmd.String("principal")
	if principal == "" {
		principal = randomPrincipal()
	}
	rcv, err := mcpreceiver.Listen(mcpreceiver.Options{Addr: cmd.String("listen"), PublicURL: cmd.String("public-url"), Profile: profile})
	if err != nil {
		return fmt.Errorf("receiver: %w (is mcp-dev receive running?)", err)
	}
	defer rcv.Close()

	o := mcpcheck.Options{
		ServerURL:       cmd.String("server"),
		Principal:       principal,
		SecondPrincipal: cmd.String("second-principal"),
		Event:           cmd.String("event"),
		Arguments:       objs[0],
		Data:            objs[1],
		OtherData:       objs[2],
		TTL:             time.Duration(cmd.Int("ttl-ms")) * time.Millisecond,
		Receiver:        rcv,
		Wait:            cmd.Duration("wait"),
		Logf:            stderrf,
	}
	if tenant := cmd.String("publish-tenant"); tenant != "" {
		o.Publish = c.publisher(tenant)
	}
	report := mcpcheck.Run(ctx, o)
	if cmd.Bool("json") {
		out, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Print(report.String())
	}
	if report.Failed() {
		return exitError{1}
	}
	return nil
}

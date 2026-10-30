package main

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpserver"
	"github.com/urfave/cli/v3"
)

func serveCommand() *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "Run a reference MCP Events server in front of Outpost's /mcp routes",
		Flags: append([]cli.Flag{
			&cli.StringFlag{Name: "listen", Value: "127.0.0.1:4500", Usage: "listen address"},
			&cli.StringSliceFlag{Name: "principal", Usage: "NAME=TENANT[:TOPIC,...]: bearer token NAME acts for TENANT, limited to the topics; repeatable"},
			&cli.StringFlag{Name: "tenant", Value: "demo", Usage: "any other bearer token acts for this tenant"},
		}, outpostFlags()...),
		Action: serve,
	}
}

func parsePrincipal(s string) (string, mcpserver.Principal, error) {
	name, rest, ok := strings.Cut(s, "=")
	if !ok || name == "" || rest == "" {
		return "", mcpserver.Principal{}, usagef("--principal %q: want NAME=TENANT[:TOPIC,...]", s)
	}
	tenant, topics, _ := strings.Cut(rest, ":")
	if tenant == "" {
		return "", mcpserver.Principal{}, usagef("--principal %q: empty tenant", s)
	}
	p := mcpserver.Principal{Tenant: tenant}
	if topics != "" {
		p.AllowedTopics = strings.Split(topics, ",")
	}
	return name, p, nil
}

func serve(ctx context.Context, cmd *cli.Command) error {
	c, err := outpostConn(cmd)
	if err != nil {
		return err
	}
	principals := map[string]mcpserver.Principal{}
	for _, s := range cmd.StringSlice("principal") {
		name, p, err := parsePrincipal(s)
		if err != nil {
			return err
		}
		principals[name] = p
	}
	handler := mcpserver.New(mcpserver.Config{
		OutpostURL:    c.URL,
		APIKey:        c.APIKey,
		Principals:    principals,
		DefaultTenant: cmd.String("tenant"),
		Header:        c.Header,
		Logf:          stderrf,
	})
	mux := http.NewServeMux()
	mux.Handle("GET /samples/events.json", samplesHandler())
	mux.Handle("/", handler)
	srv := &http.Server{Addr: cmd.String("listen"), Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	stderrf("serving MCP Events on http://%s/ (Outpost %s)", srv.Addr, c.URL)
	stderrf("sample events on http://%s/samples/events.json", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

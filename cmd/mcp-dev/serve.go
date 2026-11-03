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
			&cli.StringFlag{Name: "listen", Value: "127.0.0.1:4500", Sources: cli.EnvVars("MCP_DEV_LISTEN"), Usage: "listen address"},
			&cli.StringSliceFlag{Name: "user", Usage: "NAME=TENANT[:TOPIC,...]: bearer token NAME acts for TENANT, limited to the topics; repeatable. Any other bearer token is the ID of its own tenant"},
		}, outpostFlags()...),
		Action: serve,
	}
}

func parseUser(s string) (string, mcpserver.User, error) {
	name, rest, ok := strings.Cut(s, "=")
	if !ok || name == "" || rest == "" {
		return "", mcpserver.User{}, usagef("--user %q: want NAME=TENANT[:TOPIC,...]", s)
	}
	tenant, topics, _ := strings.Cut(rest, ":")
	if tenant == "" {
		return "", mcpserver.User{}, usagef("--user %q: empty tenant", s)
	}
	p := mcpserver.User{Tenant: tenant}
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
	users := map[string]mcpserver.User{}
	for _, s := range cmd.StringSlice("user") {
		name, p, err := parseUser(s)
		if err != nil {
			return err
		}
		users[name] = p
	}
	handler := mcpserver.New(mcpserver.Config{
		OutpostURL:    c.URL,
		APIKey:        c.APIKey,
		Users:         users,
		TokenIsTenant: true,
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

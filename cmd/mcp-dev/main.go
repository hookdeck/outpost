// Command mcp-dev is an experimental toolkit for developing and testing MCP
// Events (draft SEP-3415) servers, clients and webhook receivers.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

type usageError struct{ error }

func usagef(format string, args ...any) error { return usageError{fmt.Errorf(format, args...)} }

// exitError ends the process with a status without printing anything.
type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func command(c *cli.Command) *cli.Command {
	c.OnUsageError = func(_ context.Context, _ *cli.Command, err error, _ bool) error { return usageError{err} }
	for _, sub := range c.Commands {
		command(sub)
	}
	return c
}

func newApp() *cli.Command {
	return command(&cli.Command{
		Name:  "mcp-dev",
		Usage: "Experimental: develop and test MCP Events (draft SEP-3415) servers, clients and receivers.",
		Commands: []*cli.Command{
			serveCommand(),
			clientCommand(),
			receiveCommand(),
			checkCommand(),
		},
	})
}

func main() {
	err := newApp().Run(context.Background(), os.Args)
	var (
		usage usageError
		exit  exitError
	)
	switch {
	case err == nil:
	case errors.As(err, &exit):
		os.Exit(exit.code)
	case errors.As(err, &usage):
		fmt.Fprintln(os.Stderr, "Error:", usage.error)
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

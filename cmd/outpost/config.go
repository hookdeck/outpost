package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/urfave/cli/v3"
)

// newConfigCommand builds the `outpost config` subcommand tree.
func newConfigCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Inspect the effective configuration",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Path to config file",
				Sources: cli.EnvVars("CONFIG"),
			},
		},
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "Print the effective, resolved configuration with secrets masked",
				Action: runConfigList,
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return cli.ShowSubcommandHelp(c)
		},
	}
}

func runConfigList(ctx context.Context, c *cli.Command) error {
	flags := config.Flags{Config: c.String("config")}

	// Load without validation: an operator reaching for `config list` is
	// often trying to see why the server won't start (a missing required
	// field), so failing validation must not also hide the resolved config.
	cfg, err := config.LoadWithoutValidation(flags)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Reuse the same masked field set written to the startup logs, so the
	// CLI output and the logs can never drift apart.
	printConfigList(os.Stdout, cfg.LogConfigurationSummary())

	if err := cfg.Validate(flags); err != nil {
		fmt.Fprintf(os.Stderr, "\nWarning: config is invalid: %v\n", err)
	}
	return nil
}

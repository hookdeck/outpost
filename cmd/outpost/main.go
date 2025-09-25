package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hookdeck/outpost/internal/version"
	"github.com/urfave/cli/v3"
)

func main() {
	app := &cli.Command{
		Name:    "outpost",
		Usage:   "Outpost - Event delivery platform",
		Version: version.Version(),
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "Run the Outpost server",
				Action: func(ctx context.Context, c *cli.Command) error {
					return delegateToBinary("outpost-server", c)
				},
			},
			{
				Name:  "migrate",
				Usage: "Migration tools",
				Commands: []*cli.Command{
					{
						Name:  "redis",
						Usage: "Run Redis migrations",
						Action: func(ctx context.Context, c *cli.Command) error {
							// Pass all arguments directly to the redis migration tool
							return delegateToBinary("outpost-migrate-redis", c)
						},
						// Don't define flags here - let the sub-binary handle everything
						SkipFlagParsing: true,
					},
				},
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			// Default action if no command specified - show help
			if c.NArg() == 0 {
				return cli.ShowAppHelp(c)
			}

			// Handle direct binary invocation for backward compatibility
			// e.g., "outpost --some-flag" defaults to server mode
			return delegateToBinary("outpost-server", c)
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		os.Exit(1)
	}
}

func delegateToBinary(binaryName string, c *cli.Command) error {
	// Find the binary
	binary, err := findBinary(binaryName)
	if err != nil {
		// In development mode, fall back to go run
		return runWithGo(binaryName, c)
	}

	// Build arguments
	args := buildArgs(c)

	// Execute the binary
	cmd := exec.Command(binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func runWithGo(binaryName string, c *cli.Command) error {
	// Map binary names to their cmd directories
	cmdPath := map[string]string{
		"outpost-server":        "./cmd/outpost-server",
		"outpost-migrate-redis": "./cmd/outpost-migrate-redis",
	}

	path, ok := cmdPath[binaryName]
	if !ok {
		return fmt.Errorf("unknown binary: %s", binaryName)
	}

	// Build arguments for go run
	args := []string{"run", path}
	args = append(args, buildArgs(c)...)

	// Execute with go run
	cmd := exec.Command("go", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func findBinary(name string) (string, error) {
	// First, try to find it in the same directory as this binary
	execPath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(execPath)
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// Then try PATH
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("binary %s not found in the same directory or PATH", name)
}

func buildArgs(c *cli.Command) []string {
	// If SkipFlagParsing is set, just pass through all raw arguments
	if c.Command("") != nil && c.Command("").SkipFlagParsing {
		// Get all args after the command name
		return c.Args().Slice()
	}

	var args []string

	// Get the root command
	root := c.Root()

	// Add global flags from root context if they are set
	for _, flag := range root.Flags {
		flagName := flag.Names()[0]
		if c.IsSet(flagName) {
			switch flag.(type) {
			case *cli.StringFlag:
				if val := c.String(flagName); val != "" {
					args = append(args, fmt.Sprintf("--%s=%s", flagName, val))
				}
			case *cli.BoolFlag:
				if c.Bool(flagName) {
					args = append(args, fmt.Sprintf("--%s", flagName))
				}
			case *cli.IntFlag:
				if c.IsSet(flagName) {
					args = append(args, fmt.Sprintf("--%s=%d", flagName, c.Int(flagName)))
				}
			}
		}
	}

	// Get the current command being executed
	currentCmd := c.Command("") // Get current command info

	// Add command-specific flags
	if currentCmd != nil && currentCmd.Flags != nil {
		for _, flag := range currentCmd.Flags {
			flagName := flag.Names()[0]
			if c.IsSet(flagName) {
				switch flag.(type) {
				case *cli.StringFlag:
					if val := c.String(flagName); val != "" {
						args = append(args, fmt.Sprintf("--%s=%s", flagName, val))
					}
				case *cli.BoolFlag:
					if c.Bool(flagName) {
						args = append(args, fmt.Sprintf("--%s", flagName))
					}
				case *cli.IntFlag:
					args = append(args, fmt.Sprintf("--%s=%d", flagName, c.Int(flagName)))
				}
			}
		}
	}

	// Pass through any additional arguments
	if c.NArg() > 0 {
		args = append(args, c.Args().Slice()...)
	}

	return args
}

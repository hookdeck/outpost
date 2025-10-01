// TODO: improve CLI parsing

package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/migrator"
)

func main() {
	ctx := context.Background()
	flags := config.ParseFlags()
	cfg, err := config.Parse(flags)
	if err != nil {
		handleErr(err)
		return
	}

	if err := run(ctx, cfg); err != nil {
		handleErr(err)
	}
}

func run(ctx context.Context, cfg *config.Config) error {
	migrator, err := migrator.New(cfg.ToMigratorOpts())
	if err != nil {
		return err
	}
	defer closeMigrator(migrator)

	command := os.Args[1]
	switch command {
	case "up":
		// $ outpost migration up
		// $ outpost migration up 1 // apply 1 migration

		var stepCount int
		if len(os.Args) < 3 {
			stepCount = -1
		} else {
			stepCount, err = strconv.Atoi(os.Args[2])
			if err != nil {
				return err
			}
		}

		_, versionJumped, err := migrator.Up(ctx, stepCount)
		if err != nil {
			return err
		}
		if versionJumped > 0 {
			fmt.Printf("migrations applied: %d\n", versionJumped)
		} else {
			fmt.Println("no migrations applied")
		}
	case "down":
		var stepCount int
		if len(os.Args) < 3 {
			stepCount = -1
		} else {
			stepCount, err = strconv.Atoi(os.Args[2])
			if err != nil {
				return err
			}
		}

		_, versionRolledBack, err := migrator.Down(ctx, stepCount)
		if err != nil {
			return err
		}
		if versionRolledBack > 0 {
			fmt.Printf("migrations rolled back: %d\n", versionRolledBack)
		} else {
			fmt.Println("no migrations rolled back")
		}
	case "force":
		// $ outpost migration force <version>

		if len(os.Args) < 3 {
			return fmt.Errorf("version is required")
		}

		version, err := strconv.Atoi(os.Args[2])
		if err != nil {
			return err
		}

		if err := migrator.Force(ctx, version); err != nil {
			return err
		}
		fmt.Println("migrations forced")

	default:
		return fmt.Errorf("invalid command: %s", command)
	}
	return nil
}

func closeMigrator(migrator *migrator.Migrator) {
	sourceErr, dbErr := migrator.Close(context.Background())
	if sourceErr != nil {
		fmt.Fprintf(os.Stderr, "%s\n", sourceErr)
	}
	if dbErr != nil {
		fmt.Fprintf(os.Stderr, "%s\n", dbErr)
	}
}

func handleErr(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(1)
}

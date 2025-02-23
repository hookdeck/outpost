package migrator

import (
	"context"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/clickhouse"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/postgres/*.sql
var pgMigrations embed.FS

//go:embed migrations/clickhouse/*.sql
var chMigrations embed.FS

type Migrator struct {
	migrate *migrate.Migrate
}

func New(opts MigrationOpts) (*Migrator, error) {
	if err := opts.validate(); err != nil {
		return nil, fmt.Errorf("invalid migration opts: %w", err)
	}

	d, err := opts.getDriver()
	if err != nil {
		return nil, fmt.Errorf("failed to get migration driver: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, opts.databaseURL())
	if err != nil {
		return nil, fmt.Errorf("migrate.New: %w", err)
	}

	return &Migrator{
		migrate: m,
	}, nil
}

func (m *Migrator) Version(ctx context.Context) (int, error) {
	version, _, err := m.migrate.Version()
	if err != nil {
		if err == migrate.ErrNilVersion {
			return 0, nil
		}
		return 0, fmt.Errorf("migrate.Version: %w", err)
	}
	return int(version), nil
}

// Up migrates the database up by n migrations. It returns the updated version,
// the number of migrations applied, and an error.
func (m *Migrator) Up(ctx context.Context, n int) (int, int, error) {
	initVersion, err := m.Version(ctx)
	if err != nil {
		return 0, 0, err
	}

	if n < 0 {
		// migrate up
		if err := m.migrate.Up(); err != nil {
			if err == migrate.ErrNoChange {
				return initVersion, 0, nil
			}
			return initVersion, 0, fmt.Errorf("migrate.Up: %w", err)
		}
	} else {
		// migrate up n migrations
		if err := m.migrate.Steps(n); err != nil {
			return initVersion, 0, fmt.Errorf("migrate.Steps: %w", err)
		}
	}

	version, err := m.Version(ctx)
	if err != nil {
		return initVersion, 0, fmt.Errorf("Error reading version after migration: %w", err)
	}

	return version, version - initVersion, nil
}

// Down migrates the database down by n migrations. It returns the updated version,
// the number of migrations rolled back, and an error.
func (m *Migrator) Down(ctx context.Context, n int) (int, int, error) {
	fmt.Println("down", n)

	initVersion, err := m.Version(ctx)
	if err != nil {
		return 0, 0, err
	}

	if n > 0 {
		if n > initVersion {
			return initVersion, 0, fmt.Errorf("cannot rollback more migrations than current version; current version: %d, n: %d", initVersion, n)
		}

		// rollback n migrations
		if err := m.migrate.Steps(n * -1); err != nil {
			return initVersion, 0, fmt.Errorf("migrate.Steps: %w", err)
		}
	} else {
		// rollback all migrations
		if err := m.migrate.Down(); err != nil {
			if err == migrate.ErrNoChange {
				return initVersion, 0, nil
			}
			return initVersion, 0, fmt.Errorf("migrate.Down: %w", err)
		}
	}

	version, err := m.Version(ctx)
	if err != nil {
		return initVersion, 0, fmt.Errorf("Error reading version after migration: %w", err)
	}

	return version, initVersion - version, nil
}

func (m *Migrator) Close(ctx context.Context) (error, error) {
	return m.migrate.Close()
}

type MigrationOptsPG struct {
	URL string
}

type MigrationOptsCH struct {
	Addr     string
	Username string
	Password string
	Database string
}

type MigrationOpts struct {
	PG MigrationOptsPG
	CH MigrationOptsCH
}

func (opts *MigrationOpts) validate() error {
	if opts.PG.URL != "" {
		return nil
	}

	if opts.CH.Addr != "" {
		return nil
	}

	return fmt.Errorf("invalid migration opts")
}

func (opts *MigrationOpts) getDriver() (source.Driver, error) {
	if opts.PG.URL != "" {
		d, err := iofs.New(pgMigrations, "migrations/postgres")
		if err != nil {
			return nil, fmt.Errorf("failed to create postgres migration source: %w", err)
		}
		return d, nil
	}

	if opts.CH.Addr != "" {
		d, err := iofs.New(chMigrations, "migrations/clickhouse")
		if err != nil {
			return nil, fmt.Errorf("failed to create clickhouse migration source: %w", err)
		}
		return d, nil
	}

	return nil, fmt.Errorf("no migration source available")
}

func (opts *MigrationOpts) databaseURL() string {
	if opts.PG.URL != "" {
		return opts.PG.URL
	}

	if opts.CH.Addr != "" {
		return fmt.Sprintf("clickhouse://%s:%s@%s/%s?x-multi-statement=true", opts.CH.Username, opts.CH.Password, opts.CH.Addr, opts.CH.Database)
	}

	return ""
}

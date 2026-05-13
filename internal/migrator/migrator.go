package migrator

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

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

	// sourceFS and sourceDir reference the embedded migration files
	// so introspection helpers (ListMigrations, LatestVersion, PendingCount)
	// can enumerate available migrations without going through the driver.
	sourceFS  fs.FS
	sourceDir string
}

// MigrationInfo describes a single SQL migration and its applied state.
type MigrationInfo struct {
	Version int
	Name    string
	Applied bool
}

func New(opts MigrationOpts) (*Migrator, error) {
	if err := opts.validate(); err != nil {
		return nil, fmt.Errorf("invalid migration opts: %w", err)
	}

	d, err := opts.getDriver()
	if err != nil {
		return nil, fmt.Errorf("failed to get migration driver: %w", err)
	}

	dbURL := opts.databaseURL()
	m, err := migrate.NewWithSourceInstance("iofs", d, dbURL)
	if err != nil {
		// Sanitize the error to prevent credential exposure in logs
		// The original error from golang-migrate may contain the full database URL
		// with credentials, which would be exposed if this error is logged.
		return nil, sanitizeConnectionError(err, dbURL)
	}

	sourceFS, sourceDir := opts.sourceLocation()

	return &Migrator{
		migrate:   m,
		sourceFS:  sourceFS,
		sourceDir: sourceDir,
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

// ListMigrations returns all available SQL migrations in version order,
// with Applied set based on the migrator's current schema version.
func (m *Migrator) ListMigrations(ctx context.Context) ([]MigrationInfo, error) {
	available, err := m.availableMigrations()
	if err != nil {
		return nil, err
	}

	currentVersion, err := m.Version(ctx)
	if err != nil {
		return nil, err
	}

	for i := range available {
		available[i].Applied = available[i].Version <= currentVersion
	}

	return available, nil
}

// LatestVersion returns the highest migration version available in the
// embedded migration files. Returns 0 if there are no migrations.
func (m *Migrator) LatestVersion() (int, error) {
	available, err := m.availableMigrations()
	if err != nil {
		return 0, err
	}
	if len(available) == 0 {
		return 0, nil
	}
	return available[len(available)-1].Version, nil
}

// PendingCount returns the number of migrations that would be applied by
// running Up(-1) from the current state.
func (m *Migrator) PendingCount(ctx context.Context) (int, error) {
	current, err := m.Version(ctx)
	if err != nil {
		return 0, err
	}
	latest, err := m.LatestVersion()
	if err != nil {
		return 0, err
	}
	if latest <= current {
		return 0, nil
	}
	return latest - current, nil
}

// availableMigrations enumerates migration files from the embedded FS.
// Files are expected to be named "NNNNNN_name.up.sql" (and corresponding
// .down.sql). Only .up.sql files are counted. The result is sorted by version.
func (m *Migrator) availableMigrations() ([]MigrationInfo, error) {
	if m.sourceFS == nil {
		return nil, fmt.Errorf("migrator has no source filesystem")
	}

	entries, err := fs.ReadDir(m.sourceFS, m.sourceDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations dir %q: %w", m.sourceDir, err)
	}

	result := make([]MigrationInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, ok := parseMigrationFilename(entry.Name())
		if !ok {
			continue
		}
		result = append(result, info)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Version < result[j].Version
	})

	return result, nil
}

// parseMigrationFilename parses filenames of the form "NNNNNN_name.up.sql".
// Files that do not match this pattern (including .down.sql files) return
// ok=false and should be ignored by the caller.
func parseMigrationFilename(filename string) (MigrationInfo, bool) {
	if !strings.HasSuffix(filename, ".up.sql") {
		return MigrationInfo{}, false
	}
	trimmed := strings.TrimSuffix(filename, ".up.sql")

	idx := strings.Index(trimmed, "_")
	if idx <= 0 || idx == len(trimmed)-1 {
		return MigrationInfo{}, false
	}

	version, err := strconv.Atoi(trimmed[:idx])
	if err != nil {
		return MigrationInfo{}, false
	}

	return MigrationInfo{
		Version: version,
		Name:    trimmed[idx+1:],
	}, true
}

type MigrationOptsPG struct {
	URL string
}

type MigrationOptsCH struct {
	Addr         string
	Username     string
	Password     string
	Database     string
	DeploymentID string
	TLSEnabled   bool
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
		prefix := ""
		if opts.CH.DeploymentID != "" {
			prefix = opts.CH.DeploymentID + "_"
		}
		src := newDeploymentSource(chMigrations, "migrations/clickhouse", prefix)
		d, err := iofs.New(src, ".")
		if err != nil {
			return nil, fmt.Errorf("failed to create clickhouse migration source: %w", err)
		}
		return d, nil
	}

	return nil, fmt.Errorf("no migration source available")
}

// sourceLocation returns the raw embedded filesystem and subdirectory for
// the configured database type. It is used by introspection helpers that
// enumerate migration files directly (without driver wrapping).
func (opts *MigrationOpts) sourceLocation() (fs.FS, string) {
	if opts.PG.URL != "" {
		return pgMigrations, "migrations/postgres"
	}
	if opts.CH.Addr != "" {
		return chMigrations, "migrations/clickhouse"
	}
	return nil, ""
}

func (m *Migrator) Force(ctx context.Context, version int) error {
	return m.migrate.Force(version)
}

func (opts *MigrationOpts) databaseURL() string {
	if opts.PG.URL != "" {
		return opts.PG.URL
	}

	if opts.CH.Addr != "" {
		// clickhouse-go v1 (used by golang-migrate) expects credentials as query params.
		// MergeTree engine is used for broader compatibility (TinyLog is not supported everywhere).
		connURL := fmt.Sprintf("clickhouse://%s/%s?username=%s&password=%s&x-multi-statement=true&x-migrations-table-engine=MergeTree",
			opts.CH.Addr,
			opts.CH.Database,
			url.QueryEscape(opts.CH.Username),
			url.QueryEscape(opts.CH.Password))
		if opts.CH.TLSEnabled {
			connURL += "&secure=true"
		}
		if opts.CH.DeploymentID != "" {
			connURL += "&x-migrations-table=" + url.QueryEscape(opts.CH.DeploymentID) + "_schema_migrations"
		}
		return connURL
	}

	return ""
}

// deploymentSource wraps an embed.FS and replaces {deployment_prefix} placeholders
// in SQL files with the actual deployment prefix. This enables multiple deployments
// to share the same ClickHouse database with isolated table names.
type deploymentSource struct {
	fsys   embed.FS
	subdir string
	prefix string
}

func newDeploymentSource(fsys embed.FS, subdir, prefix string) *deploymentSource {
	return &deploymentSource{
		fsys:   fsys,
		subdir: subdir,
		prefix: prefix,
	}
}

// Open implements fs.FS
func (d *deploymentSource) Open(name string) (fs.File, error) {
	// Map the path to the embedded subdir
	path := name
	if d.subdir != "" && name != "." {
		path = d.subdir + "/" + name
	} else if name == "." {
		path = d.subdir
	}

	f, err := d.fsys.Open(path)
	if err != nil {
		return nil, err
	}

	// For directories, return as-is
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.IsDir() {
		return &deploymentDir{File: f, d: d, path: path}, nil
	}

	// For SQL files, wrap to replace placeholders
	if strings.HasSuffix(name, ".sql") {
		content, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		replaced := strings.ReplaceAll(string(content), "{deployment_prefix}", d.prefix)
		return &deploymentFile{
			Reader: strings.NewReader(replaced),
			info:   &deploymentFileInfo{name: info.Name(), size: int64(len(replaced)), mode: info.Mode(), modTime: info.ModTime()},
		}, nil
	}

	return f, nil
}

// deploymentDir wraps a directory to return modified DirEntry names
type deploymentDir struct {
	fs.File
	d    *deploymentSource
	path string
}

func (dd *deploymentDir) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(dd.d.fsys, dd.path)
	if err != nil {
		return nil, err
	}
	if n > 0 && n < len(entries) {
		entries = entries[:n]
	}
	return entries, nil
}

// deploymentFile wraps file content with replaced placeholders
type deploymentFile struct {
	*strings.Reader
	info *deploymentFileInfo
}

func (f *deploymentFile) Stat() (fs.FileInfo, error) {
	return f.info, nil
}

func (f *deploymentFile) Close() error {
	return nil
}

// deploymentFileInfo provides file info for replaced content
type deploymentFileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

func (i *deploymentFileInfo) Name() string       { return i.name }
func (i *deploymentFileInfo) Size() int64        { return i.size }
func (i *deploymentFileInfo) Mode() fs.FileMode  { return i.mode }
func (i *deploymentFileInfo) ModTime() time.Time { return i.modTime }
func (i *deploymentFileInfo) IsDir() bool        { return false }
func (i *deploymentFileInfo) Sys() any           { return nil }

package migrator

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/golang-migrate/migrate/v4/source"
)

// deploymentSource wraps an embed.FS and replaces {deployment_prefix} placeholders
// with deployment-specific prefixes. It implements the source.Driver interface for golang-migrate.
type deploymentSource struct {
	fs           fs.FS
	path         string
	deploymentID string
	migrations   *source.Migrations
}

// newDeploymentSource creates a new deployment source driver.
// It reads migrations from the embedded FS and replaces "{deployment_prefix}" placeholder:
// - If deploymentID is set: {deployment_prefix} -> {deploymentID}_
// - If deploymentID is empty: {deployment_prefix} -> "" (empty string)
func newDeploymentSource(fsys fs.FS, path string, deploymentID string) (source.Driver, error) {
	ds := &deploymentSource{
		fs:           fsys,
		path:         path,
		deploymentID: deploymentID,
		migrations:   source.NewMigrations(),
	}

	if err := ds.init(); err != nil {
		return nil, err
	}

	return ds, nil
}

func (ds *deploymentSource) init() error {
	entries, err := fs.ReadDir(ds.fs, ds.path)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		m, err := source.DefaultParse(name)
		if err != nil {
			continue // skip files that don't match migration pattern
		}

		if !ds.migrations.Append(m) {
			return fmt.Errorf("unable to parse migration file: %s", name)
		}
	}

	return nil
}

// Open is part of the source.Driver interface.
func (ds *deploymentSource) Open(url string) (source.Driver, error) {
	return nil, fmt.Errorf("Open is not implemented for deploymentSource; use newDeploymentSource instead")
}

// Close is part of the source.Driver interface.
func (ds *deploymentSource) Close() error {
	return nil
}

// First returns the first migration version.
func (ds *deploymentSource) First() (version uint, err error) {
	v, ok := ds.migrations.First()
	if !ok {
		return 0, &fs.PathError{Op: "first", Path: ds.path, Err: fs.ErrNotExist}
	}
	return v, nil
}

// Prev returns the previous migration version.
func (ds *deploymentSource) Prev(version uint) (prevVersion uint, err error) {
	v, ok := ds.migrations.Prev(version)
	if !ok {
		return 0, &fs.PathError{Op: "prev", Path: ds.path, Err: fs.ErrNotExist}
	}
	return v, nil
}

// Next returns the next migration version.
func (ds *deploymentSource) Next(version uint) (nextVersion uint, err error) {
	v, ok := ds.migrations.Next(version)
	if !ok {
		return 0, &fs.PathError{Op: "next", Path: ds.path, Err: fs.ErrNotExist}
	}
	return v, nil
}

// ReadUp reads the up migration for the given version and performs deployment suffix replacement.
func (ds *deploymentSource) ReadUp(version uint) (r io.ReadCloser, identifier string, err error) {
	m, ok := ds.migrations.Up(version)
	if !ok {
		return nil, "", &fs.PathError{Op: "readup", Path: ds.path, Err: fs.ErrNotExist}
	}

	content, err := ds.readAndTransform(m.Raw)
	if err != nil {
		return nil, "", err
	}

	return io.NopCloser(bytes.NewReader(content)), m.Identifier, nil
}

// ReadDown reads the down migration for the given version and performs deployment suffix replacement.
func (ds *deploymentSource) ReadDown(version uint) (r io.ReadCloser, identifier string, err error) {
	m, ok := ds.migrations.Down(version)
	if !ok {
		return nil, "", &fs.PathError{Op: "readdown", Path: ds.path, Err: fs.ErrNotExist}
	}

	content, err := ds.readAndTransform(m.Raw)
	if err != nil {
		return nil, "", err
	}

	return io.NopCloser(bytes.NewReader(content)), m.Identifier, nil
}

// readAndTransform reads a migration file and replaces deployment placeholders.
func (ds *deploymentSource) readAndTransform(filename string) ([]byte, error) {
	filepath := ds.path + "/" + filename
	content, err := fs.ReadFile(ds.fs, filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read migration file %s: %w", filepath, err)
	}

	// Replace {deployment_prefix} with actual prefix (or empty string)
	transformed := ds.replaceDeploymentPrefix(string(content))
	return []byte(transformed), nil
}

// replaceDeploymentPrefix replaces "{deployment_prefix}" placeholder with the actual prefix.
// If deploymentID is set, it becomes "{deploymentID}_". Otherwise, it becomes empty string.
func (ds *deploymentSource) replaceDeploymentPrefix(sql string) string {
	prefix := ""
	if ds.deploymentID != "" {
		prefix = fmt.Sprintf("%s_", ds.deploymentID)
	}
	return strings.ReplaceAll(sql, "{deployment_prefix}", prefix)
}

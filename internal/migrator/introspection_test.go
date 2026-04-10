package migrator

import "testing"

func TestParseMigrationFilename(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		wantOK      bool
		wantVersion int
		wantName    string
	}{
		{
			name:        "standard up migration",
			filename:    "000001_init.up.sql",
			wantOK:      true,
			wantVersion: 1,
			wantName:    "init",
		},
		{
			name:        "multi-word name",
			filename:    "000005_denormalize_attempts.up.sql",
			wantOK:      true,
			wantVersion: 5,
			wantName:    "denormalize_attempts",
		},
		{
			name:     "down migration is ignored",
			filename: "000001_init.down.sql",
			wantOK:   false,
		},
		{
			name:     "unrelated file is ignored",
			filename: "README.md",
			wantOK:   false,
		},
		{
			name:     "missing version prefix",
			filename: "_orphan.up.sql",
			wantOK:   false,
		},
		{
			name:     "non-numeric version",
			filename: "abcdef_name.up.sql",
			wantOK:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info, ok := parseMigrationFilename(tc.filename)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if info.Version != tc.wantVersion {
				t.Errorf("version = %d, want %d", info.Version, tc.wantVersion)
			}
			if info.Name != tc.wantName {
				t.Errorf("name = %q, want %q", info.Name, tc.wantName)
			}
		})
	}
}

// TestAvailableMigrations_Postgres exercises availableMigrations against the
// real embedded postgres migrations. It does not require a running database.
func TestAvailableMigrations_Postgres(t *testing.T) {
	m := &Migrator{
		sourceFS:  pgMigrations,
		sourceDir: "migrations/postgres",
	}

	available, err := m.availableMigrations()
	if err != nil {
		t.Fatalf("availableMigrations: %v", err)
	}

	if len(available) == 0 {
		t.Fatal("expected at least one embedded postgres migration")
	}

	// Versions must be strictly increasing.
	for i := 1; i < len(available); i++ {
		if available[i].Version <= available[i-1].Version {
			t.Errorf("migrations not sorted: %d <= %d",
				available[i].Version, available[i-1].Version)
		}
	}

	// First migration should be the init migration at version 1.
	if available[0].Version != 1 {
		t.Errorf("first migration version = %d, want 1", available[0].Version)
	}
	if available[0].Name != "init" {
		t.Errorf("first migration name = %q, want %q", available[0].Name, "init")
	}
}

func TestAvailableMigrations_Clickhouse(t *testing.T) {
	m := &Migrator{
		sourceFS:  chMigrations,
		sourceDir: "migrations/clickhouse",
	}

	available, err := m.availableMigrations()
	if err != nil {
		t.Fatalf("availableMigrations: %v", err)
	}

	if len(available) == 0 {
		t.Fatal("expected at least one embedded clickhouse migration")
	}
}

func TestLatestVersion(t *testing.T) {
	m := &Migrator{
		sourceFS:  pgMigrations,
		sourceDir: "migrations/postgres",
	}

	latest, err := m.LatestVersion()
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}

	if latest < 1 {
		t.Errorf("latest = %d, want >= 1", latest)
	}
}

func TestAvailableMigrations_MissingSourceFS(t *testing.T) {
	m := &Migrator{}
	_, err := m.availableMigrations()
	if err == nil {
		t.Fatal("expected error when sourceFS is nil")
	}
}

func TestSourceLocation(t *testing.T) {
	tests := []struct {
		name    string
		opts    MigrationOpts
		wantDir string
		wantNil bool
	}{
		{
			name:    "postgres",
			opts:    MigrationOpts{PG: MigrationOptsPG{URL: "postgres://x"}},
			wantDir: "migrations/postgres",
		},
		{
			name:    "clickhouse",
			opts:    MigrationOpts{CH: MigrationOptsCH{Addr: "localhost:9000"}},
			wantDir: "migrations/clickhouse",
		},
		{
			name:    "empty",
			opts:    MigrationOpts{},
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fsys, dir := tc.opts.sourceLocation()
			if tc.wantNil {
				if fsys != nil {
					t.Errorf("fsys = %v, want nil", fsys)
				}
				return
			}
			if fsys == nil {
				t.Error("fsys is nil")
			}
			if dir != tc.wantDir {
				t.Errorf("dir = %q, want %q", dir, tc.wantDir)
			}
		})
	}
}

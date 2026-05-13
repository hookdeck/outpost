// Package coordinator provides a unified interface over the SQL and Redis
// migration subsystems so callers (CLI, startup checks) can work with
// "migrations" as a single concept instead of two separate systems.
package coordinator

import "time"

// MigrationType identifies the storage backend a migration targets.
type MigrationType string

const (
	MigrationTypeSQL   MigrationType = "sql"
	MigrationTypeRedis MigrationType = "redis"
)

// MigrationStatus is the lifecycle state of a single migration.
type MigrationStatus string

const (
	StatusPending       MigrationStatus = "pending"
	StatusApplied       MigrationStatus = "applied"
	StatusNotApplicable MigrationStatus = "not_applicable"
)

// MigrationInfo is the unified view of a single migration across both
// SQL and Redis subsystems.
type MigrationInfo struct {
	// ID is a stable identifier used for output/reference, e.g.
	// "sql/000003" or "redis/001_hash_tags".
	ID string

	// Type indicates which subsystem owns this migration.
	Type MigrationType

	// Version orders migrations within a given Type.
	Version int

	// Name is a human-readable name (e.g. "init", "hash_tags").
	Name string

	// Description is a longer human-readable explanation. May be empty
	// for SQL migrations (which only have names).
	Description string

	// Status reflects the current state as tracked by the underlying
	// subsystem.
	Status MigrationStatus

	// Reason is populated when Status is StatusNotApplicable.
	Reason string
}

// ApplyOptions controls an Apply invocation.
type ApplyOptions struct {
	// SQLOnly skips Redis migrations.
	SQLOnly bool
	// RedisOnly skips SQL migrations.
	RedisOnly bool
}

// Plan is the aggregated view of what Apply would do right now.
type Plan struct {
	// SQL summarizes pending SQL migrations.
	SQL SQLPlan
	// Redis lists pending Redis migrations that are applicable.
	Redis []RedisMigrationPlan
}

// HasChanges returns true if applying the plan would do anything.
func (p *Plan) HasChanges() bool {
	return p.SQL.PendingCount > 0 || len(p.Redis) > 0
}

// SQLPlan summarizes what would happen in the SQL subsystem.
type SQLPlan struct {
	CurrentVersion int
	LatestVersion  int
	PendingCount   int
	Pending        []SQLMigrationInfo
}

// SQLMigrationInfo is a single SQL migration that is pending.
type SQLMigrationInfo struct {
	Version int
	Name    string
}

// RedisMigrationPlan is the plan output for a single Redis migration.
type RedisMigrationPlan struct {
	Name           string
	Description    string
	EstimatedItems int
	Scope          map[string]int
}

// PendingSummary is a lightweight view used by the startup gate.
type PendingSummary struct {
	SQLPending   int
	RedisPending int
}

// HasPending returns true if any migrations are pending across subsystems.
func (p PendingSummary) HasPending() bool {
	return p.SQLPending > 0 || p.RedisPending > 0
}

// VerificationReport is the result of a Verify invocation.
type VerificationReport struct {
	SQLCurrentVersion int
	SQLLatestVersion  int
	RedisResults      []RedisVerifyResult
}

// Ok returns true if no issues were reported.
func (r *VerificationReport) Ok() bool {
	if r.SQLCurrentVersion != r.SQLLatestVersion {
		return false
	}
	for _, rr := range r.RedisResults {
		if !rr.Valid {
			return false
		}
	}
	return true
}

// RedisVerifyResult captures the outcome of verifying a single Redis migration.
type RedisVerifyResult struct {
	Name         string
	Valid        bool
	ChecksRun    int
	ChecksPassed int
	Issues       []string
	VerifiedAt   time.Time
}

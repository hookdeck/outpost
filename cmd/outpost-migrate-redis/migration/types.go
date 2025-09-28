package migration

import (
	"context"
	"time"
)

// Migration defines the interface that all migrations must implement
type Migration interface {
	// Name returns the migration identifier (e.g., "001_hash_tags")
	Name() string

	// Version returns the schema version this migration upgrades to (1, 2, 3, etc.)
	// For example, migration 001 upgrades from v0 to v1, so it returns 1
	Version() int

	// Description returns a human-readable description
	Description() string

	// Plan analyzes the current state and returns a migration plan
	Plan(ctx context.Context) (*Plan, error)

	// Apply executes the migration based on the plan
	Apply(ctx context.Context, plan *Plan) (*State, error)

	// Verify validates that the migration was successful
	Verify(ctx context.Context, state *State) (*VerificationResult, error)

	// Cleanup removes old data after successful verification
	// Should not prompt for confirmation - that's handled by the CLI
	Cleanup(ctx context.Context, state *State) error
}

// Plan represents a migration plan
type Plan struct {
	MigrationName  string            `json:"migration_name"`
	Description    string            `json:"description"`
	Version        string            `json:"version"`
	Timestamp      time.Time         `json:"timestamp"`
	Scope          map[string]int    `json:"scope"` // e.g., {"tenants": 100, "destinations": 500}
	EstimatedItems int               `json:"estimated_items"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// State represents the current state of a migration
type State struct {
	MigrationName string                 `json:"migration_name"`
	Phase         string                 `json:"phase"` // planned, applied, verified, cleaned
	StartedAt     time.Time              `json:"started_at"`
	CompletedAt   *time.Time             `json:"completed_at,omitempty"`
	Progress      Progress               `json:"progress"`
	Errors        []string               `json:"errors,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// Progress tracks migration progress
type Progress struct {
	TotalItems     int `json:"total_items"`
	ProcessedItems int `json:"processed_items"`
	FailedItems    int `json:"failed_items"`
	SkippedItems   int `json:"skipped_items"`
}

// VerificationResult contains verification results
type VerificationResult struct {
	Valid        bool              `json:"valid"`
	ChecksRun    int               `json:"checks_run"`
	ChecksPassed int               `json:"checks_passed"`
	Issues       []string          `json:"issues,omitempty"`
	Details      map[string]string `json:"details,omitempty"`
}

// No registry needed - migrations are created directly in the Migrator

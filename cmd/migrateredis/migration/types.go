package migration

import (
	"context"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
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
	Plan(ctx context.Context, client redis.Client, verbose bool) (*Plan, error)

	// Apply executes the migration based on the plan
	Apply(ctx context.Context, client redis.Client, plan *Plan, verbose bool) (*State, error)

	// Verify validates that the migration was successful
	Verify(ctx context.Context, client redis.Client, state *State, verbose bool) (*VerificationResult, error)

	// Cleanup removes old data after successful verification
	Cleanup(ctx context.Context, client redis.Client, state *State, force bool, verbose bool) error
}

// Plan represents a migration plan
type Plan struct {
	MigrationName    string            `json:"migration_name"`
	Description      string            `json:"description"`
	Version          string            `json:"version"`
	Timestamp        time.Time         `json:"timestamp"`
	Scope            map[string]int    `json:"scope"` // e.g., {"tenants": 100, "destinations": 500}
	EstimatedItems   int               `json:"estimated_items"`
	Metadata         map[string]string `json:"metadata,omitempty"`
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
	Valid       bool              `json:"valid"`
	ChecksRun   int               `json:"checks_run"`
	ChecksPassed int              `json:"checks_passed"`
	Issues      []string          `json:"issues,omitempty"`
	Details     map[string]string `json:"details,omitempty"`
}

// Registry manages available migrations
type Registry struct {
	migrations map[string]Migration
}

// NewRegistry creates a new migration registry
func NewRegistry() *Registry {
	return &Registry{
		migrations: make(map[string]Migration),
	}
}

// Register adds a migration to the registry
func (r *Registry) Register(m Migration) {
	r.migrations[m.Name()] = m
}

// Get retrieves a migration by name
func (r *Registry) Get(name string) (Migration, bool) {
	m, ok := r.migrations[name]
	return m, ok
}

// List returns all registered migration names
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.migrations))
	for name := range r.migrations {
		names = append(names, name)
	}
	return names
}

// GetAll returns all registered migrations
func (r *Registry) GetAll() map[string]Migration {
	return r.migrations
}
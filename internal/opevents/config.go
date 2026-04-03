package opevents

// Config holds the configuration for the operation events system.
// yaml/env tags live in internal/config; this is the domain-level struct.
type Config struct {
	Topics []string
}

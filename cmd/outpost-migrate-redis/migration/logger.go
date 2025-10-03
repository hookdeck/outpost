package migration

// Logger provides a simple logging interface for migrations
// Each migration receives a logger and can check verbosity
type Logger interface {
	// Verbose returns true if verbose/debug logging is enabled
	Verbose() bool

	// LogProgress reports migration progress
	LogProgress(current, total int, item string)

	// LogInfo logs informational messages (always shown)
	LogInfo(msg string)

	// LogDebug logs debug information (only shown if Verbose() is true)
	LogDebug(msg string)

	// LogWarning logs warning messages
	LogWarning(msg string)

	// LogError logs error messages
	LogError(msg string, err error)
}

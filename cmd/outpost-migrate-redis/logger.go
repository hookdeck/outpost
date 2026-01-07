package main

import (
	"errors"

	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/urfave/cli/v3"
)

// ErrNonInteractiveMode is returned when interactive operations are attempted in non-interactive mode
var ErrNonInteractiveMode = errors.New("operation requires interactive mode")

// MigrationStats holds statistics about a completed migration
type MigrationStats struct {
	ProcessedItems int
	FailedItems    int
	SkippedItems   int
	Duration       string
}

// MigrationLogger provides migration-specific logging methods
type MigrationLogger interface {
	// Verbose returns true if verbose logging is enabled
	Verbose() bool

	// Configuration and initialization
	LogRedisConfig(host string, port int, database int, clusterEnabled bool, tlsEnabled bool, hasPassword bool)
	LogInitialization(isFresh bool, migrationsMarked int)

	// Migration lifecycle
	LogMigrationList(migrations map[string]string) // name -> description
	LogMigrationStatus(applied, pending int)
	LogMigrationStart(name string)
	LogMigrationPlan(name string, plan *migratorredis.Plan)
	LogMigrationProgress(name string, current, total int, tenant string)
	LogMigrationComplete(name string, stats MigrationStats)
	LogMigrationCancelled()

	// Verification and cleanup
	LogVerificationStart(name string)
	LogVerificationResult(name string, result *migratorredis.VerificationResult)
	LogCleanupStart(name string)
	LogCleanupAnalysis(estimatedKeys int)
	LogCleanupComplete(keysDeleted int)
	LogNoCleanupNeeded()

	// Lock management
	LogLockAcquiring(operation string)
	LogLockAcquired()
	LogLockReleased()
	LogLockWaiting()
	LogLockStatus(lockInfo string, exists bool)
	LogLockCleared()

	// Redis state
	LogCheckingInstallation()
	LogFreshInstallation()
	LogExistingInstallation()
	LogPendingMigrations(count int)
	LogAllMigrationsApplied()

	// Special formatted messages
	LogNoMigrationsNeeded() // Used in Plan when no migrations are pending

	// General logging
	LogInfo(msg string)
	LogWarning(msg string)
	LogError(msg string, err error)
	LogDebug(msg string)

	// Generic progress logging (implements migration.Logger interface)
	LogProgress(current, total int, item string)

	// Interactive operations (return error if non-interactive)
	Confirm(msg string) (bool, error)
	ConfirmWithWarning(warning, prompt string) (bool, error)
	Prompt(msg string) (string, error)
}

// CreateMigrationLogger creates the appropriate logger based on the execution context
func CreateMigrationLogger(c *cli.Command, cfg *config.Config) (MigrationLogger, error) {
	verbose := c.Bool("verbose")
	logFormat := c.String("log-format")

	// Use JSON logging if explicitly requested
	if logFormat == "json" {
		return createServerLogger(cfg, verbose)
	}

	// Default to CLI logger for text format
	return NewCLILogger(verbose), nil
}

// createServerLogger creates a structured logger for server context
func createServerLogger(cfg *config.Config, verbose bool) (MigrationLogger, error) {
	// Determine log level
	logLevel := "info"
	auditLog := false

	if cfg != nil {
		if cfg.LogLevel != "" {
			logLevel = cfg.LogLevel
		}
		auditLog = cfg.AuditLog
	}

	// Always use internal/logging package
	logger, err := logging.NewLogger(
		logging.WithLogLevel(logLevel),
		logging.WithAuditLog(auditLog),
	)
	if err != nil {
		return nil, err
	}

	return NewServerLogger(logger, verbose), nil
}

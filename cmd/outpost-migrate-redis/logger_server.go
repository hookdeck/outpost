package main

import (
	"strings"

	"github.com/hookdeck/outpost/cmd/outpost-migrate-redis/migration"
	"github.com/hookdeck/outpost/internal/logging"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ServerLogger implements MigrationLogger with structured logging for server/production context
type ServerLogger struct {
	logger  *logging.Logger
	verbose bool
}

// NewServerLogger creates a new server logger with OTEL support
func NewServerLogger(logger *logging.Logger, verbose bool) *ServerLogger {
	return &ServerLogger{
		logger:  logger,
		verbose: verbose,
	}
}

// Verbose returns true if verbose logging is enabled
func (l *ServerLogger) Verbose() bool {
	return l.verbose
}

// Configuration and initialization
func (l *ServerLogger) LogRedisConfig(host string, port int, database int, clusterEnabled bool, tlsEnabled bool, hasPassword bool) {
	if !l.verbose {
		return
	}
	l.logger.Debug("redis configuration",
		zap.String("host", host),
		zap.Int("port", port),
		zap.Int("database", database),
		zap.Bool("cluster_enabled", clusterEnabled),
		zap.Bool("tls_enabled", tlsEnabled),
		zap.Bool("password_set", hasPassword),
	)
}

func (l *ServerLogger) LogInitialization(isFresh bool, migrationsMarked int) {
	if isFresh {
		l.logger.Info("redis initialized successfully",
			zap.Bool("fresh_installation", true),
			zap.Int("migrations_marked", migrationsMarked),
		)
	} else {
		l.logger.Info("redis already initialized")
	}
}

// Migration lifecycle
func (l *ServerLogger) LogMigrationList(migrations map[string]string) {
	if l.verbose {
		l.logger.Debug("available migrations",
			zap.Int("count", len(migrations)),
			zap.Any("migrations", migrations),
		)
	}
}

func (l *ServerLogger) LogMigrationStatus(applied, pending int) {
	l.logger.Info("migration status",
		zap.Int("applied", applied),
		zap.Int("pending", pending),
	)
}

func (l *ServerLogger) LogMigrationStart(name string) {
	l.logger.Info("starting migration",
		zap.String("migration", name),
	)
}

func (l *ServerLogger) LogMigrationPlan(name string, plan *migration.Plan) {
	fields := []zap.Field{
		zap.String("migration", name),
		zap.String("description", plan.Description),
		zap.Int("estimated_items", plan.EstimatedItems),
	}

	if l.verbose && len(plan.Scope) > 0 {
		fields = append(fields, zap.Any("scope", plan.Scope))
	}

	l.logger.Info("migration plan", fields...)
}

func (l *ServerLogger) LogMigrationProgress(name string, current, total int, tenant string) {
	// Only log progress at intervals to avoid log spam
	shouldLog := l.verbose || (current%100 == 0) || (current == total)
	if !shouldLog {
		return
	}

	level := zapcore.DebugLevel
	if current == total {
		level = zapcore.InfoLevel
	}

	fields := []zap.Field{
		zap.String("migration", name),
		zap.Int("current", current),
		zap.Int("total", total),
		zap.Float64("progress_pct", float64(current)/float64(total)*100),
	}

	if tenant != "" && l.verbose {
		fields = append(fields, zap.String("tenant_id", tenant))
	}

	l.logger.Log(level, "migration progress", fields...)
}

func (l *ServerLogger) LogMigrationComplete(name string, stats MigrationStats) {
	l.logger.Info("migration completed",
		zap.String("migration", name),
		zap.Int("processed_items", stats.ProcessedItems),
		zap.Int("failed_items", stats.FailedItems),
		zap.Int("skipped_items", stats.SkippedItems),
		zap.String("duration", stats.Duration),
	)
}

func (l *ServerLogger) LogMigrationCancelled() {
	l.logger.Info("migration cancelled")
}

// Verification and cleanup
func (l *ServerLogger) LogVerificationStart(name string) {
	l.logger.Info("starting verification",
		zap.String("migration", name),
	)
}

func (l *ServerLogger) LogVerificationResult(name string, result *migration.VerificationResult) {
	fields := []zap.Field{
		zap.String("migration", name),
		zap.Bool("valid", result.Valid),
		zap.Int("checks_run", result.ChecksRun),
		zap.Int("checks_passed", result.ChecksPassed),
	}

	if len(result.Issues) > 0 {
		fields = append(fields, zap.Strings("issues", result.Issues))
	}

	if l.verbose && len(result.Details) > 0 {
		fields = append(fields, zap.Any("details", result.Details))
	}

	if result.Valid {
		l.logger.Info("verification passed", fields...)
	} else {
		l.logger.Warn("verification failed", fields...)
	}
}

func (l *ServerLogger) LogCleanupStart(name string) {
	l.logger.Info("starting cleanup",
		zap.String("migration", name),
	)
}

func (l *ServerLogger) LogCleanupAnalysis(estimatedKeys int) {
	if l.verbose {
		l.logger.Debug("analyzing cleanup",
			zap.Int("estimated_keys", estimatedKeys),
		)
	}
}

func (l *ServerLogger) LogCleanupComplete(keysDeleted int) {
	l.logger.Info("cleanup completed",
		zap.Int("keys_deleted", keysDeleted),
	)
}

func (l *ServerLogger) LogNoCleanupNeeded() {
	l.logger.Info("no cleanup needed")
}

// Lock management
func (l *ServerLogger) LogLockAcquiring(operation string) {
	l.logger.Debug("acquiring lock",
		zap.String("operation", operation),
	)
}

func (l *ServerLogger) LogLockAcquired() {
	if l.verbose {
		l.logger.Debug("lock acquired")
	}
}

func (l *ServerLogger) LogLockReleased() {
	if l.verbose {
		l.logger.Debug("lock released")
	}
}

func (l *ServerLogger) LogLockWaiting() {
	l.logger.Info("waiting for lock",
		zap.String("reason", "another process is initializing"),
	)
}

func (l *ServerLogger) LogLockStatus(lockInfo string, exists bool) {
	if exists {
		l.logger.Info("lock status",
			zap.String("lock_info", lockInfo),
			zap.Bool("exists", true),
		)
	} else {
		l.logger.Info("lock status",
			zap.Bool("exists", false),
		)
	}
}

func (l *ServerLogger) LogLockCleared() {
	l.logger.Info("lock cleared")
}

// Redis state
func (l *ServerLogger) LogCheckingInstallation() {
	l.logger.Info("checking redis installation status")
}

func (l *ServerLogger) LogFreshInstallation() {
	l.logger.Info("fresh installation detected")
}

func (l *ServerLogger) LogExistingInstallation() {
	l.logger.Info("redis already initialized")
}

func (l *ServerLogger) LogPendingMigrations(count int) {
	l.logger.Warn("migrations pending", zap.Int("count", count))
}

func (l *ServerLogger) LogAllMigrationsApplied() {
	l.logger.Info("all migrations applied")
}

// General logging
func (l *ServerLogger) LogInfo(msg string) {
	l.logger.Info(msg)
}

func (l *ServerLogger) LogWarning(msg string) {
	l.logger.Warn(msg)
}

func (l *ServerLogger) LogError(msg string, err error) {
	if err != nil {
		l.logger.Error(msg, zap.Error(err))
	} else {
		l.logger.Error(msg)
	}
}

func (l *ServerLogger) LogDebug(msg string) {
	if l.verbose {
		l.logger.Debug(msg)
	}
}

// LogProgress implements the migration.Logger interface
// This is the generic version used by migrations
func (l *ServerLogger) LogProgress(current, total int, item string) {
	// Only log progress at intervals to avoid log spam
	shouldLog := l.verbose || (current%100 == 0) || (current == total)
	if !shouldLog {
		return
	}

	level := zapcore.DebugLevel
	if current == total {
		level = zapcore.InfoLevel
	}

	fields := []zap.Field{
		zap.Int("current", current),
		zap.Int("total", total),
		zap.Float64("progress_pct", float64(current)/float64(total)*100),
	}

	if item != "" && l.verbose {
		fields = append(fields, zap.String("item", item))
	}

	l.logger.Log(level, "progress", fields...)
}

// Interactive operations - always return error in server mode
func (l *ServerLogger) Confirm(msg string) (bool, error) {
	l.logger.Debug("confirm_requested_non_interactive",
		zap.String("prompt", msg),
	)
	return false, ErrNonInteractiveMode
}

func (l *ServerLogger) ConfirmWithWarning(warning, prompt string) (bool, error) {
	l.logger.Debug("confirm_with_warning_requested_non_interactive",
		zap.String("warning", warning),
		zap.String("prompt", prompt),
	)
	return false, ErrNonInteractiveMode
}

func (l *ServerLogger) Prompt(msg string) (string, error) {
	l.logger.Debug("prompt_requested_non_interactive",
		zap.String("prompt", msg),
	)
	return "", ErrNonInteractiveMode
}

// LogNoMigrationsNeeded logs that no migrations are needed (clean format for JSON)
func (l *ServerLogger) LogNoMigrationsNeeded() {
	l.logger.Info("all migrations applied, nothing to plan")
}

// Sync flushes any buffered log entries
func (l *ServerLogger) Sync() error {
	err := l.logger.Sync()
	// Ignore common stderr sync errors that occur in containers and terminals
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "inappropriate ioctl for device") ||
			strings.Contains(errStr, "invalid argument") {
			return nil
		}
	}
	return err
}

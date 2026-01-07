package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
)

// CLILogger implements MigrationLogger for interactive CLI usage
type CLILogger struct {
	verbose bool
}

// NewCLILogger creates a new CLI logger
func NewCLILogger(verbose bool) *CLILogger {
	return &CLILogger{
		verbose: verbose,
	}
}

// Verbose returns true if verbose logging is enabled
func (l *CLILogger) Verbose() bool {
	return l.verbose
}

// Configuration and initialization
func (l *CLILogger) LogRedisConfig(host string, port int, database int, clusterEnabled bool, tlsEnabled bool, hasPassword bool) {
	if !l.verbose {
		return
	}
	fmt.Println("Redis Configuration:")
	fmt.Printf("  Host: %s\n", host)
	fmt.Printf("  Port: %d\n", port)
	fmt.Printf("  Database: %d\n", database)
	fmt.Printf("  Cluster Enabled: %v\n", clusterEnabled)
	fmt.Printf("  TLS Enabled: %v\n", tlsEnabled)
	if hasPassword {
		fmt.Printf("  Password: ****** (set)\n")
	} else {
		fmt.Printf("  Password: (not set)\n")
	}
	fmt.Println()
}

func (l *CLILogger) LogInitialization(isFresh bool, migrationsMarked int) {
	if isFresh {
		fmt.Printf("✅ Redis initialized successfully - marked %d migration(s) as applied\n", migrationsMarked)
	} else {
		fmt.Println("Redis already initialized")
	}
}

// Migration lifecycle
func (l *CLILogger) LogMigrationList(migrations map[string]string) {
	if len(migrations) == 0 {
		fmt.Println("No migrations registered")
		return
	}

	// Sort migration names for consistent output
	var names []string
	for name := range migrations {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("Available migrations:")
	for _, name := range names {
		fmt.Printf("  %s - %s\n", name, migrations[name])
	}
}

func (l *CLILogger) LogMigrationStatus(applied, pending int) {
	fmt.Println("Migration Status:")
	fmt.Printf("  Applied: %d migration(s)\n", applied)
	fmt.Printf("  Pending: %d migration(s)\n", pending)
}

func (l *CLILogger) LogMigrationStart(name string) {
	fmt.Println("Applying migration...")
}

func (l *CLILogger) LogMigrationPlan(name string, plan *migratorredis.Plan) {
	fmt.Printf("\nNext Migration: %s\n", name)
	fmt.Printf("  Description: %s\n", plan.Description)
	fmt.Printf("  Estimated items: %d\n", plan.EstimatedItems)
	if l.verbose && len(plan.Scope) > 0 {
		fmt.Println("  Scope:")
		for key, value := range plan.Scope {
			fmt.Printf("    %s: %d\n", key, value)
		}
	}
}

func (l *CLILogger) LogMigrationProgress(name string, current, total int, tenant string) {
	if l.verbose {
		if tenant != "" {
			fmt.Printf("  [%d/%d] Migrating tenant: %s\n", current, total, tenant)
		} else {
			fmt.Printf("Progress: %d/%d tenants\n", current, total)
		}
	} else if current%10 == 0 || current == total {
		// Show progress every 10 items or at completion in non-verbose mode
		fmt.Printf("\rProgress: %d/%d items", current, total)
		if current == total {
			fmt.Println() // New line at completion
		}
	}
}

func (l *CLILogger) LogMigrationComplete(name string, stats MigrationStats) {
	fmt.Printf("Migration completed successfully.\n")
	fmt.Printf("  Processed items: %d\n", stats.ProcessedItems)
	if stats.FailedItems > 0 {
		fmt.Printf("  Failed items: %d\n", stats.FailedItems)
	}
	if stats.SkippedItems > 0 {
		fmt.Printf("  Skipped items: %d\n", stats.SkippedItems)
	}
}

func (l *CLILogger) LogMigrationCancelled() {
	fmt.Println("Migration cancelled.")
}

// Verification and cleanup
func (l *CLILogger) LogVerificationStart(name string) {
	fmt.Printf("Verifying migration %s...\n", name)
}

func (l *CLILogger) LogVerificationResult(name string, result *migratorredis.VerificationResult) {
	if result.Valid {
		fmt.Println("✅ Migration verified successfully")
		if l.verbose {
			fmt.Printf("  Checks run: %d\n", result.ChecksRun)
			fmt.Printf("  Checks passed: %d\n", result.ChecksPassed)
		}
	} else {
		fmt.Println("❌ Migration verification failed")
		fmt.Printf("  Checks run: %d\n", result.ChecksRun)
		fmt.Printf("  Checks passed: %d\n", result.ChecksPassed)
		if len(result.Issues) > 0 {
			fmt.Println("  Issues found:")
			for _, issue := range result.Issues {
				fmt.Printf("    - %s\n", issue)
			}
		}
	}

	if l.verbose && len(result.Details) > 0 {
		fmt.Println("  Details:")
		for key, value := range result.Details {
			fmt.Printf("    %s: %s\n", key, value)
		}
	}
}

func (l *CLILogger) LogCleanupStart(name string) {
	fmt.Printf("Cleaning up old keys from migration %s...\n", name)
}

func (l *CLILogger) LogCleanupAnalysis(estimatedKeys int) {
	fmt.Printf("Analyzing cleanup for migration...\n")
	if estimatedKeys > 0 && l.verbose {
		fmt.Printf("  Estimated keys to remove: %d\n", estimatedKeys)
	}
}

func (l *CLILogger) LogCleanupComplete(keysDeleted int) {
	fmt.Println("✅ Cleanup completed successfully")
	fmt.Printf("  Keys removed: %d\n", keysDeleted)
}

func (l *CLILogger) LogNoCleanupNeeded() {
	fmt.Println("No old keys to cleanup.")
}

// Lock management
func (l *CLILogger) LogLockAcquiring(operation string) {
	fmt.Printf("Acquiring lock for %s...\n", operation)
}

func (l *CLILogger) LogLockAcquired() {
	if l.verbose {
		fmt.Println("Lock acquired successfully")
	}
}

func (l *CLILogger) LogLockReleased() {
	if l.verbose {
		fmt.Println("Lock released")
	}
}

func (l *CLILogger) LogLockWaiting() {
	fmt.Println("Another process is initializing, waiting...")
}

func (l *CLILogger) LogLockStatus(lockInfo string, exists bool) {
	if exists {
		fmt.Printf("Current lock: %s\n", lockInfo)
	} else {
		fmt.Println("No migration lock found")
	}
}

func (l *CLILogger) LogLockCleared() {
	fmt.Println("✅ Migration lock cleared")
}

// Redis state
func (l *CLILogger) LogCheckingInstallation() {
	fmt.Println("Checking Redis installation status...")
}

func (l *CLILogger) LogFreshInstallation() {
	fmt.Println("Fresh installation detected, acquiring lock...")
}

func (l *CLILogger) LogExistingInstallation() {
	fmt.Println("Redis already initialized")
}

func (l *CLILogger) LogPendingMigrations(count int) {
	fmt.Fprintf(os.Stderr, "Migration required: %d pending\n", count)
}

func (l *CLILogger) LogAllMigrationsApplied() {
	fmt.Println("All migrations have been applied. Nothing to do.")
}

// General logging
func (l *CLILogger) LogInfo(msg string) {
	fmt.Println(msg)
}

func (l *CLILogger) LogWarning(msg string) {
	fmt.Printf("⚠️  %s\n", msg)
}

func (l *CLILogger) LogError(msg string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s: %v\n", msg, err)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	}
}

func (l *CLILogger) LogDebug(msg string) {
	if l.verbose {
		fmt.Printf("[DEBUG] %s\n", msg)
	}
}

// LogProgress implements the migration.Logger interface
// This is the generic version used by migrations
func (l *CLILogger) LogProgress(current, total int, item string) {
	if l.verbose {
		if item != "" {
			fmt.Printf("  [%d/%d] Processing: %s\n", current, total, item)
		} else {
			fmt.Printf("  [%d/%d] Processing...\n", current, total)
		}
	} else if current%10 == 0 || current == total {
		fmt.Printf("\rProgress: %d/%d", current, total)
		if current == total {
			fmt.Println()
		}
	}
}

// Interactive operations
func (l *CLILogger) Confirm(msg string) (bool, error) {
	fmt.Printf("%s (y/N): ", msg)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		response := strings.ToLower(strings.TrimSpace(scanner.Text()))
		return response == "y" || response == "yes", nil
	}
	return false, scanner.Err()
}

func (l *CLILogger) ConfirmWithWarning(warning, prompt string) (bool, error) {
	fmt.Printf("⚠️  WARNING: %s\n", warning)
	return l.Confirm(prompt)
}

func (l *CLILogger) Prompt(msg string) (string, error) {
	fmt.Print(msg)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	return "", scanner.Err()
}

// LogNoMigrationsNeeded logs that no migrations are needed (with nice formatting for CLI)
func (l *CLILogger) LogNoMigrationsNeeded() {
	fmt.Println("\nAll migrations have been applied. Nothing to plan.")
}

// Sync flushes any buffered log entries (no-op for CLI logger)
func (l *CLILogger) Sync() error {
	// CLI logger writes directly to stdout/stderr, no buffering
	return nil
}

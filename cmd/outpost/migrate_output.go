package main

import (
	"fmt"
	"io"

	"github.com/hookdeck/outpost/internal/migrator/coordinator"
)

// printMigrationList renders the output for `outpost migrate list`.
func printMigrationList(w io.Writer, list []coordinator.MigrationInfo) {
	var sql, redis []coordinator.MigrationInfo
	for _, m := range list {
		switch m.Type {
		case coordinator.MigrationTypeSQL:
			sql = append(sql, m)
		case coordinator.MigrationTypeRedis:
			redis = append(redis, m)
		}
	}

	var sqlPending, redisPending int

	if len(sql) > 0 {
		fmt.Fprintln(w, "SQL Migrations:")
		for _, m := range sql {
			fmt.Fprintf(w, "  [%-14s] sql/%06d  %s\n", m.Status, m.Version, m.Name)
			if m.Status == coordinator.StatusPending {
				sqlPending++
			}
		}
	}

	if len(redis) > 0 {
		if len(sql) > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "Redis Migrations:")
		for _, m := range redis {
			line := fmt.Sprintf("  [%-14s] redis/%s", m.Status, m.Name)
			if m.Description != "" {
				line += "  " + m.Description
			}
			fmt.Fprintln(w, line)
			if m.Reason != "" {
				fmt.Fprintf(w, "                    reason: %s\n", m.Reason)
			}
			if m.Status == coordinator.StatusPending {
				redisPending++
			}
		}
	}

	fmt.Fprintf(w, "\nSummary: %d SQL pending, %d Redis pending\n", sqlPending, redisPending)
}

// printMigrationPlan renders the output for `outpost migrate plan`.
func printMigrationPlan(w io.Writer, plan *coordinator.Plan) {
	if !plan.HasChanges() {
		fmt.Fprintln(w, "All migrations are up to date.")
		return
	}

	fmt.Fprintln(w, "Planning migrations...")
	fmt.Fprintln(w)

	if plan.SQL.PendingCount > 0 {
		fmt.Fprintf(w, "SQL Migrations (%d pending, v%d → v%d):\n",
			plan.SQL.PendingCount, plan.SQL.CurrentVersion, plan.SQL.LatestVersion)
		for _, m := range plan.SQL.Pending {
			fmt.Fprintf(w, "  - sql/%06d  %s\n", m.Version, m.Name)
		}
	} else {
		fmt.Fprintln(w, "SQL Migrations: up to date")
	}

	if len(plan.Redis) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Redis Migrations (%d pending):\n", len(plan.Redis))
		for _, m := range plan.Redis {
			fmt.Fprintf(w, "  - redis/%s: %s\n", m.Name, m.Description)
			if m.EstimatedItems > 0 {
				fmt.Fprintf(w, "      estimated operations: %d\n", m.EstimatedItems)
			}
			for k, v := range m.Scope {
				fmt.Fprintf(w, "      %s: %d\n", k, v)
			}
		}
	} else {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Redis Migrations: up to date")
	}
}

// printVerificationReport renders the output for `outpost migrate verify`.
func printVerificationReport(w io.Writer, report *coordinator.VerificationReport) {
	fmt.Fprintln(w, "Verifying migrations...")
	fmt.Fprintln(w)

	status := "OK"
	if report.SQLCurrentVersion != report.SQLLatestVersion {
		status = "BEHIND"
	}
	fmt.Fprintf(w, "SQL: current=%d latest=%d [%s]\n",
		report.SQLCurrentVersion, report.SQLLatestVersion, status)

	if len(report.RedisResults) > 0 {
		fmt.Fprintln(w, "Redis:")
		for _, r := range report.RedisResults {
			mark := "OK"
			if !r.Valid {
				mark = "FAIL"
			}
			fmt.Fprintf(w, "  redis/%s: %s (%d/%d checks passed)\n",
				r.Name, mark, r.ChecksPassed, r.ChecksRun)
			for _, issue := range r.Issues {
				fmt.Fprintf(w, "    - %s\n", issue)
			}
		}
	}

	fmt.Fprintln(w)
	if report.Ok() {
		fmt.Fprintln(w, "All migrations verified successfully.")
	} else {
		fmt.Fprintln(w, "Verification reported issues.")
	}
}

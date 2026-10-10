package entx

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"time"

	"eino-quickstart/ent"
	"entgo.io/ent/dialect/sql/schema"
)

// The advisory lock serializes the explicit migration command without making
// application startup acquire a DDL lock. The value is private to this
// service's database and is stable across deployments.
const migrationLockKey int64 = 781238491

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Migration struct {
	Version     string `json:"version"`
	Description string `json:"description"`
	SQL         string `json:"-"`
}

type MigrationStatus struct {
	Migration
	Applied   bool       `json:"applied"`
	AppliedAt *time.Time `json:"appliedAt,omitempty"`
}

var migrationPlan = []Migration{
	{Version: "0001_schema_migrations", Description: "create the idempotent migration ledger", SQL: "migrations/0001_schema_migrations.sql"},
	{Version: "0002_user_nickname_backfill", Description: "backfill legacy user nicknames", SQL: "migrations/0002_user_nickname_backfill.sql"},
	{Version: "0003_user_external_id", Description: "add nullable stable upstream user identity", SQL: "migrations/0003_user_external_id.sql"},
}

// PlannedMigrations returns the checked-in migration plan without exposing the
// SQL body to callers that only need to render a dry-run.
func PlannedMigrations() []Migration {
	plan := make([]Migration, len(migrationPlan))
	copy(plan, migrationPlan)
	for i := range plan {
		plan[i].SQL = ""
	}
	return plan
}

func loadMigrationPlan() ([]Migration, error) {
	plan := make([]Migration, len(migrationPlan))
	copy(plan, migrationPlan)
	for i := range plan {
		body, err := migrationFiles.ReadFile(plan[i].SQL)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", plan[i].Version, err)
		}
		plan[i].SQL = string(body)
	}
	return plan, nil
}

// Migrate performs only additive Ent schema reconciliation and checked-in SQL
// migrations. It never enables WithDropColumn or WithDropIndex.
func Migrate(ctx context.Context, client *ent.Client) error {
	if client == nil {
		return errors.New("migrate postgres: nil client")
	}
	plan, err := loadMigrationPlan()
	if err != nil {
		return err
	}
	return withMigrationLock(ctx, client, func() error {
		// The ledger itself is the bootstrap statement, so status and reruns are
		// available even when this is the first migration against a database.
		if _, err := client.ExecContext(ctx, plan[0].SQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", plan[0].Version, err)
		}
		if err := markApplied(ctx, client, plan[0].Version); err != nil {
			return err
		}

		// Ent is used only for additive reconciliation. This intentionally does
		// not pass any drop options.
		if err := client.Schema.Create(ctx, schema.WithForeignKeys(false)); err != nil {
			return fmt.Errorf("reconcile additive ent schema: %w", err)
		}

		for _, migration := range plan[1:] {
			applied, err := isApplied(ctx, client, migration.Version)
			if err != nil {
				return err
			}
			if applied {
				continue
			}
			if _, err := client.ExecContext(ctx, migration.SQL); err != nil {
				return fmt.Errorf("apply migration %s: %w", migration.Version, err)
			}
			if err := markApplied(ctx, client, migration.Version); err != nil {
				return err
			}
		}
		return nil
	})
}

// Status returns the checked-in migration plan joined with the database ledger.
func Status(ctx context.Context, client *ent.Client) ([]MigrationStatus, error) {
	if client == nil {
		return nil, errors.New("migration status: nil client")
	}
	rows, err := client.QueryContext(ctx, `SELECT version, applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read migration ledger: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]time.Time)
	for rows.Next() {
		var version string
		var at time.Time
		if err := rows.Scan(&version, &at); err != nil {
			return nil, fmt.Errorf("scan migration ledger: %w", err)
		}
		applied[version] = at
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migration ledger: %w", err)
	}

	plan := PlannedMigrations()
	status := make([]MigrationStatus, 0, len(plan))
	for _, migration := range plan {
		entry := MigrationStatus{Migration: migration}
		if at, ok := applied[migration.Version]; ok {
			atCopy := at
			entry.Applied = true
			entry.AppliedAt = &atCopy
		}
		status = append(status, entry)
	}
	return status, nil
}

func withMigrationLock(ctx context.Context, client *ent.Client, fn func() error) error {
	if _, err := client.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() { _, _ = client.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockKey) }()
	return fn()
}

func isApplied(ctx context.Context, client *ent.Client, version string) (bool, error) {
	rows, err := client.QueryContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version)
	if err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("check migration %s: %w", version, err)
		}
		return false, fmt.Errorf("check migration %s: empty result", version)
	}
	var exists bool
	if err := rows.Scan(&exists); err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}
	return exists, nil
}

func markApplied(ctx context.Context, client *ent.Client, version string) error {
	_, err := client.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT (version) DO NOTHING`, version)
	if err != nil {
		return fmt.Errorf("record migration %s: %w", version, err)
	}
	return nil
}

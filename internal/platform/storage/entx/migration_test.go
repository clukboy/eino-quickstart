package entx

import (
	"strings"
	"testing"
)

func TestMigrationPlanIsEmbeddedAndNonDestructive(t *testing.T) {
	plan, err := loadMigrationPlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) < 2 {
		t.Fatalf("migration plan has %d entries, want at least 2", len(plan))
	}
	for _, migration := range plan {
		if strings.TrimSpace(migration.SQL) == "" {
			t.Fatalf("migration %s has empty SQL", migration.Version)
		}
		upper := strings.ToUpper(migration.SQL)
		if strings.Contains(upper, "DROP COLUMN") || strings.Contains(upper, "DROP INDEX") {
			t.Fatalf("migration %s contains a destructive drop", migration.Version)
		}
	}
}

func TestPlannedMigrationsDoesNotExposeSQL(t *testing.T) {
	for _, migration := range PlannedMigrations() {
		if migration.SQL != "" {
			t.Fatalf("dry-run plan should not expose SQL body for %s", migration.Version)
		}
	}
}

// Command db-migrate is the explicit database deployment step.
// Application processes must not run this command implicitly during startup.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/storage/entx"
)

type output struct {
	Mode        string                 `json:"mode"`
	Config      string                 `json:"config"`
	Migrations  []entx.MigrationStatus `json:"migrations,omitempty"`
	Plan        []entx.Migration       `json:"plan,omitempty"`
	AppliedNote string                 `json:"note,omitempty"`
}

func main() {
	configPath := flag.String("config", config.WorkerConfigPath(), "API or worker business config")
	profile := flag.String("profile", "worker", "config profile: worker, api, or legacy")
	dryRun := flag.Bool("dry-run", false, "print the checked-in migration plan without connecting")
	apply := flag.Bool("apply", false, "apply additive migrations to the configured database")
	status := flag.Bool("status", false, "read migration status from the configured database")
	flag.Parse()

	selected := 0
	for _, enabled := range []bool{*dryRun, *apply, *status} {
		if enabled {
			selected++
		}
	}
	if selected != 1 {
		log.Fatal("choose exactly one of -dry-run, -apply, or -status")
	}

	if *dryRun {
		writeJSON(output{
			Mode:        "dry-run",
			Config:      *configPath,
			Plan:        entx.PlannedMigrations(),
			AppliedNote: "no database connection or DDL was performed",
		})
		return
	}

	cfg, err := loadConfig(*profile, *configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	ctx := context.Background()
	client, err := entx.OpenWithoutMigration(ctx, cfg.Storage)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer client.Close()

	if *apply {
		if err := entx.Migrate(ctx, client); err != nil {
			log.Fatalf("apply migrations: %v", err)
		}
	}
	entries, err := entx.Status(ctx, client)
	if err != nil {
		log.Fatalf("read migration status: %v", err)
	}
	mode := "status"
	if *apply {
		mode = "apply"
	}
	writeJSON(output{Mode: mode, Config: *configPath, Migrations: entries})
}

func loadConfig(profile, path string) (*config.Config, error) {
	switch profile {
	case "worker":
		return config.LoadWorker(path)
	case "api":
		return config.LoadAPI(path)
	case "legacy":
		return config.Load(path)
	default:
		return nil, fmt.Errorf("unknown -profile %q (want worker, api, or legacy)", profile)
	}
}

func writeJSON(value output) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		log.Fatal(err)
	}
}

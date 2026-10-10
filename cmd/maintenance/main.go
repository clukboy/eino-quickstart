package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eino-quickstart/internal/maintenance"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/platform/persistence/checkpoint"
	"eino-quickstart/internal/platform/persistence/turn"
	"eino-quickstart/internal/platform/privacy"
	"eino-quickstart/internal/platform/storage/entx"
)

func main() {
	configPath := flag.String("config", config.WorkerConfigPath(), "worker business config")
	dryRun := flag.Bool("dry-run", false, "report eligible records without modifying data")
	periodic := flag.Bool("periodic", false, "run cleanup immediately and then on the configured interval")
	reportPath := flag.String("report", "", "write JSON report to this path; stdout when empty")
	flag.Parse()

	cfg, err := config.LoadWorker(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	client, err := entx.OpenWithoutMigration(ctx, cfg.Storage)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	argumentPolicy, err := privacy.NewArgumentPolicy(cfg.Security.MaxApprovalArgumentBytes, cfg.Security.SensitiveArgumentKeys)
	if err != nil {
		log.Fatal(err)
	}
	if *periodic && *dryRun {
		log.Fatal("-periodic cannot be combined with -dry-run")
	}
	writeReport := func(report maintenance.Report) {
		data, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			log.Printf("maintenance report marshal failed: %v", marshalErr)
			return
		}
		if *reportPath == "" {
			fmt.Println(string(data))
			return
		}
		if writeErr := os.WriteFile(*reportPath, append(data, '\n'), 0o600); writeErr != nil {
			log.Printf("maintenance report write failed: %v", writeErr)
			return
		}
		fmt.Printf("maintenance report: %s\n", *reportPath)
	}
	worker := &maintenance.Worker{
		Approvals: approval.NewStore(client, argumentPolicy, time.Duration(cfg.Security.ApprovalTTLSeconds)*time.Second), Checkpoints: checkpoint.NewStore(client), Turns: turn.NewStore(client),
		Interval:            time.Duration(cfg.Maintenance.CleanupIntervalSeconds) * time.Second,
		ApprovalRetention:   time.Duration(cfg.Maintenance.ApprovalRetentionHours) * time.Hour,
		CheckpointRetention: time.Duration(cfg.Maintenance.CheckpointRetentionHours) * time.Hour,
		TurnRetention:       time.Duration(cfg.Maintenance.TurnRetentionHours) * time.Hour,
		BatchSize:           cfg.Maintenance.CleanupBatchSize, Logger: slog.Default(),
		OnReport: writeReport,
	}
	if *periodic {
		if err := worker.Run(ctx); err != nil {
			log.Fatal(err)
		}
		return
	}
	var report maintenance.Report
	if *dryRun {
		report, err = worker.Preview(ctx)
	} else {
		report, err = worker.RunOnce(ctx)
	}
	if err != nil {
		log.Fatal(err)
	}
	writeReport(report)
}

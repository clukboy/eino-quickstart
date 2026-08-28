package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eino-quickstart/internal/maintenance"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/observability"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/platform/persistence/checkpoint"
	"eino-quickstart/internal/platform/persistence/turn"
	"eino-quickstart/internal/platform/privacy"
	"eino-quickstart/internal/platform/storage/entx"
)

func main() {
	configPath := os.Getenv("EINO_CONFIG")
	if configPath == "" {
		configPath = "./configs/config.yaml"
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	client, err := entx.Open(ctx, cfg.Storage)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	logger, err := observability.NewLogger(
		cfg.Observability.LogLevel,
		cfg.Observability.ServiceName,
		cfg.Observability.Environment,
		cfg.Observability.LogFilePath,
		cfg.Observability.LogMaxSizeMB,
		cfg.Observability.LogMaxBackups,
		cfg.Observability.LogMaxAgeDays,
	)
	if err != nil {
		log.Fatal(err)
	}

	argumentPolicy, err := privacy.NewArgumentPolicy(
		cfg.Security.MaxApprovalArgumentBytes,
		cfg.Security.SensitiveArgumentKeys,
	)
	if err != nil {
		log.Fatal(err)
	}

	worker := &maintenance.Worker{
		Approvals:           approval.NewStore(client, argumentPolicy, time.Duration(cfg.Security.ApprovalTTLSeconds)*time.Second),
		Checkpoints:         checkpoint.NewStore(client),
		Turns:               turn.NewStore(client),
		Interval:            time.Duration(cfg.Maintenance.CleanupIntervalSeconds) * time.Second,
		ApprovalRetention:   time.Duration(cfg.Maintenance.ApprovalRetentionHours) * time.Hour,
		CheckpointRetention: time.Duration(cfg.Maintenance.CheckpointRetentionHours) * time.Hour,
		TurnRetention:       time.Duration(cfg.Maintenance.TurnRetentionHours) * time.Hour,
		BatchSize:           cfg.Maintenance.CleanupBatchSize,
		Logger:              logger,
	}
	if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

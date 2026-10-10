// Command knowledge-import imports a bounded recursive Markdown/text directory.
// It is intentionally separate from the API and worker processes: the command
// plans and writes documents, while worker remains the only index consumer.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/queue/asynq"
	"eino-quickstart/internal/platform/queue/tasks"
	"eino-quickstart/internal/platform/storage/entx"
)

type noopQueue struct{}

func (noopQueue) EnqueueIndex(context.Context, uint64, uint64, tasks.IndexMode) error { return nil }

func main() {
	var (
		configPath = flag.String("config", config.DefaultAPIConfig, "API business config")
		root       = flag.String("root", "", "directory to import; this is the filesystem boundary")
		datasetID  = flag.Uint64("dataset", 0, "target dataset ID")
		dryRun     = flag.Bool("dry-run", false, "plan and compare without writing documents or queue tasks")
		reportPath = flag.String("report", "", "JSON report path; stdout when empty")
		retryPath  = flag.String("retry-report", "", "previous report; retry only its failed paths")
		excludes   = flag.String("exclude", "", "comma-separated relative directories/files to exclude")
	)
	flag.Parse()
	if strings.TrimSpace(*root) == "" || *datasetID == 0 {
		log.Fatal("-root and -dataset are required")
	}

	cfg, err := config.LoadAPI(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	ctx := context.Background()
	client, err := entx.OpenWithoutMigration(ctx, cfg.Storage)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer client.Close()

	var queue knowledge.IndexTaskQueue = noopQueue{}
	var producer *asynq.AsynqClient
	if !*dryRun {
		if !cfg.Asynq.Enabled {
			log.Fatal("asynq.enabled=false: non-dry-run import refuses to create indexing documents")
		}
		producer = asynq.NewProducer(&asynq.AsynqConf{
			Addr: cfg.Asynq.Redis.Addr, Username: cfg.Asynq.Redis.Username,
			Pass: cfg.Asynq.Redis.Password, DB: cfg.Asynq.Redis.DB,
			Enable: true, MaxRetries: cfg.Asynq.MaxRetries,
		})
		queue = asynq.NewIndexQueue(producer, asynq.IndexQueueConfig{MaxRetries: cfg.Asynq.MaxRetries})
		defer producer.Close()
	}
	service, err := knowledge.NewService(knowledge.ServiceDeps{
		Client: client, Queue: queue, MaxDocumentBytes: cfg.Knowledge.MaxDocumentBytes,
	})
	if err != nil {
		log.Fatalf("create knowledge service: %v", err)
	}

	var retry []string
	if *retryPath != "" {
		retry, err = failedPaths(*retryPath)
		if err != nil {
			log.Fatalf("read retry report: %v", err)
		}
	}
	var exclude []string
	for _, item := range strings.Split(*excludes, ",") {
		if strings.TrimSpace(item) != "" {
			exclude = append(exclude, strings.TrimSpace(item))
		}
	}
	report, err := service.ImportDirectory(ctx, knowledge.DirectoryImportConfig{
		Root: *root, DatasetID: *datasetID, DryRun: *dryRun,
		MaxBytes: int64(cfg.Knowledge.MaxDocumentBytes), Exclude: exclude, RetryPaths: retry,
	})
	if err != nil {
		log.Fatalf("import directory: %v", err)
	}
	if err := writeReport(*reportPath, report); err != nil {
		log.Fatalf("write report: %v", err)
	}
	if report.Failed > 0 {
		os.Exit(1)
	}
}

func failedPaths(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var report knowledge.DirectoryImportReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	var paths []string
	for _, file := range report.Files {
		if file.Status == knowledge.DirectoryFileFailed {
			paths = append(paths, file.Path)
		}
	}
	if len(paths) == 0 {
		return nil, errors.New("retry report contains no failed paths")
	}
	return paths, nil
}

func writeReport(path string, report *knowledge.DirectoryImportReport) error {
	var w io.Writer = os.Stdout
	var f *os.File
	if path != "" {
		var err error
		f, err = os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	if err := report.WriteJSON(w); err != nil {
		return err
	}
	if path != "" {
		fmt.Printf("report: %s\n", path)
	}
	return nil
}

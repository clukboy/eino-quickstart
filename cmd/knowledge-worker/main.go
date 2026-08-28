package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eino-quickstart/internal/knowledge"
	"eino-quickstart/internal/knowledge/embedding"
	"eino-quickstart/internal/knowledge/vectorstore"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/storage/entx"
)

func main() {
	// -------------------------------------------------------------------------
	// 1. Load config
	// -------------------------------------------------------------------------
	configPath := os.Getenv("EINO_CONFIG")
	if configPath == "" {
		configPath = "./configs/config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// -------------------------------------------------------------------------
	// 2. Create root context with graceful shutdown
	// -------------------------------------------------------------------------
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// -------------------------------------------------------------------------
	// 3. Open PostgreSQL
	// -------------------------------------------------------------------------
	entClient, err := entx.Open(ctx, cfg.Storage)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer func() {
		if err := entClient.Close(); err != nil {
			log.Printf("close database failed: %v", err)
		}
	}()

	log.Println("knowledge worker database connected")

	// -------------------------------------------------------------------------
	// 4. Create Embedder
	// -------------------------------------------------------------------------
	embedder, err := embedding.NewOpenAIEmbedder(
		embedding.OpenAIConfig{
			BaseURL:    cfg.Embedding.BaseURL,
			APIKey:     os.Getenv(cfg.Embedding.APIKeyEnv),
			Model:      cfg.Embedding.Model,
			Dimensions: cfg.Embedding.Dimensions,
			BatchSize:  cfg.Embedding.BatchSize,
		},
	)
	if err != nil {
		log.Fatalf("create embedding client: %v", err)
	}

	log.Printf(
		"embedding initialized: model=%s dimensions=%d batchSize=%d",
		cfg.Embedding.Model,
		cfg.Embedding.Dimensions,
		cfg.Embedding.BatchSize,
	)

	// -------------------------------------------------------------------------
	// 5. Create Milvus vector store
	// -------------------------------------------------------------------------
	vecStore, err := vectorstore.NewMilvusStore(
		ctx,
		vectorstore.MilvusConfig{
			Address:    cfg.Milvus.Address,
			Collection: cfg.Milvus.Collection,
			Dimensions: cfg.Embedding.Dimensions,
			MetricType: cfg.Milvus.MetricType,
		},
	)
	if err != nil {
		log.Fatalf("create Milvus store: %v", err)
	}

	defer func() {
		closeCtx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancel()

		if err := vecStore.Close(closeCtx); err != nil {
			log.Printf("close Milvus store failed: %v", err)
		}
	}()

	if err := vecStore.EnsureCollection(ctx); err != nil {
		log.Fatalf("ensure Milvus collection: %v", err)
	}

	log.Printf(
		"Milvus initialized: address=%s collection=%s metric=%s",
		cfg.Milvus.Address,
		cfg.Milvus.Collection,
		cfg.Milvus.MetricType,
	)

	// -------------------------------------------------------------------------
	// 6. Create Knowledge Loader
	// -------------------------------------------------------------------------
	loader, err := knowledge.NewLoader(
		knowledge.LoaderConfig{
			Root:             cfg.Knowledge.Root,
			MaxDocumentBytes: cfg.Knowledge.MaxDocumentBytes,
		},
	)
	if err != nil {
		log.Fatalf("create knowledge loader: %v", err)
	}

	log.Printf(
		"knowledge loader initialized: root=%s maxDocumentBytes=%d",
		cfg.Knowledge.Root,
		cfg.Knowledge.MaxDocumentBytes,
	)

	// -------------------------------------------------------------------------
	// 7. Create Knowledge Service
	// -------------------------------------------------------------------------
	svc, err := knowledge.NewService(
		knowledge.ServiceConfig{
			Client: entClient,
			Chunker: knowledge.Chunker{
				Size:    cfg.Knowledge.ChunkSizeCharacters,
				Overlap: cfg.Knowledge.ChunkOverlapChars,
			},
			MaxChunksPerDoc: cfg.Knowledge.MaxChunksPerDoc,
			EmbeddingModel:  cfg.Embedding.Model,
		},
	)
	if err != nil {
		log.Fatalf("create knowledge service: %v", err)
	}

	log.Printf(
		"knowledge service initialized: chunkSize=%d overlap=%d maxChunksPerDoc=%d",
		cfg.Knowledge.ChunkSizeCharacters,
		cfg.Knowledge.ChunkOverlapChars,
		cfg.Knowledge.MaxChunksPerDoc,
	)

	// -------------------------------------------------------------------------
	// 8. Initial full scan + ingest
	//
	// This creates:
	//   documents
	//   document_chunks
	//   vector_outbox
	//
	// Actual vector indexing is handled by IndexerWorker below.
	// -------------------------------------------------------------------------
	log.Printf(
		"starting initial knowledge ingest: root=%s",
		cfg.Knowledge.Root,
	)

	ingestResult, err := svc.IngestRoot(
		ctx,
		loader,
		"system",
		"system",
	)
	if err != nil {
		log.Fatalf("ingest knowledge root: %v", err)
	}

	log.Printf(
		"knowledge ingest completed: loaded=%d ingested=%d",
		ingestResult.Loaded,
		ingestResult.Ingested,
	)

	// -------------------------------------------------------------------------
	// 9. Create vector indexer
	//
	// All indexer parameters come from cfg.Indexer.
	// Do not hard-code them here.
	// -------------------------------------------------------------------------
	indexer, err := knowledge.NewIndexer(
		knowledge.IndexerConfig{
			Client:            entClient,
			Embedder:          embedder,
			VectorStore:       vecStore,
			BatchSize:         cfg.Indexer.BatchSize,
			LeaseDuration:     time.Duration(cfg.Indexer.LeaseDurationSeconds) * time.Second,
			MaxAttempts:       cfg.Indexer.MaxAttempts,
			InitialRetryDelay: time.Duration(cfg.Indexer.InitialRetryDelaySeconds) * time.Second,
			MaxRetryDelay:     time.Duration(cfg.Indexer.MaxRetryDelaySeconds) * time.Second,
		},
	)
	if err != nil {
		log.Fatalf("create knowledge indexer: %v", err)
	}

	log.Printf(
		"knowledge indexer initialized: batchSize=%d lease=%s maxAttempts=%d retry=%s-%s",
		cfg.Indexer.BatchSize,
		time.Duration(cfg.Indexer.LeaseDurationSeconds)*time.Second,
		cfg.Indexer.MaxAttempts,
		time.Duration(cfg.Indexer.InitialRetryDelaySeconds)*time.Second,
		time.Duration(cfg.Indexer.MaxRetryDelaySeconds)*time.Second,
	)

	// -------------------------------------------------------------------------
	// 10. Check whether the indexer worker is enabled
	// -------------------------------------------------------------------------
	if !cfg.Indexer.Enabled {
		log.Println("knowledge indexer worker is disabled")
		log.Println("knowledge worker finished")
		return
	}

	// -------------------------------------------------------------------------
	// 11. Create and run indexer worker
	//
	// IndexerWorker.Run() already:
	//   - processes immediately once
	//   - periodically processes vector_outbox
	//   - stops when ctx is cancelled
	// -------------------------------------------------------------------------
	worker := knowledge.NewIndexerWorker(
		indexer,
		time.Duration(cfg.Indexer.IntervalSeconds)*time.Second,
	)

	log.Printf(
		"knowledge indexer worker started: interval=%s",
		time.Duration(cfg.Indexer.IntervalSeconds)*time.Second,
	)

	worker.Run(ctx)

	log.Println("knowledge indexer worker stopped")
}

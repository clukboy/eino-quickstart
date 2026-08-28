package main

import (
	"context"
	"eino-quickstart/internal/knowledge"
	"eino-quickstart/internal/knowledge/embedding"
	"eino-quickstart/internal/knowledge/vectorstore"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/storage/entx"
	"os"
	"time"

	"github.com/bytedance/gopkg/util/logger"
)

func main() {
	configPath := os.Getenv("EINO_CONFIG")
	if configPath == "" {
		configPath = "./configs/config.yaml"
	}
	ctx := context.Background()
	cfg, _ := config.Load(configPath)
	entClient, _ := entx.Open(ctx, cfg.Storage)
	defer entClient.Close()

	embedder, _ := embedding.NewOpenAIEmbedder(embedding.OpenAIConfig{
		BaseURL:    cfg.Embedding.BaseURL,
		APIKey:     os.Getenv(cfg.Embedding.APIKeyEnv),
		Model:      cfg.Embedding.Model,
		Dimensions: cfg.Embedding.Dimensions,
		BatchSize:  cfg.Embedding.BatchSize,
	})
	vecStore, _ := vectorstore.NewMilvusStore(ctx, vectorstore.MilvusConfig{
		Address: cfg.Milvus.Address, Collection: cfg.Milvus.Collection,
		Dimensions: cfg.Embedding.Dimensions, MetricType: cfg.Milvus.MetricType,
	})
	vecStore.EnsureCollection(ctx)

	// 1) 启动时先做一次全量扫描 + 摄取
	loader, _ := knowledge.NewLoader(knowledge.LoaderConfig{
		Root: cfg.Knowledge.Root, MaxDocumentBytes: cfg.Knowledge.MaxDocumentBytes,
	})
	svc, _ := knowledge.NewService(knowledge.ServiceConfig{
		Client: entClient,
		Chunker: knowledge.Chunker{
			Size:    cfg.Knowledge.ChunkSizeCharacters,
			Overlap: cfg.Knowledge.ChunkOverlapChars,
		},
		MaxChunksPerDoc: cfg.Knowledge.MaxChunksPerDoc,
		EmbeddingModel:  cfg.Embedding.Model,
	})
	result, err := svc.IngestRoot(ctx, loader, "system", "system")
	logger.Info("knowledge ingest", "loaded", result.Loaded, "ingested", result.Ingested, "err", err)

	// 2) 常驻轮询消费 vector_outbox
	indexer, _ := knowledge.NewIndexer(knowledge.IndexerConfig{
		Client: entClient, Embedder: embedder, VectorStore: vecStore,
		BatchSize: 32, LeaseDuration: 30 * time.Second,
		MaxAttempts: 5, InitialRetryDelay: time.Second, MaxRetryDelay: 30 * time.Second,
	})
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			res, err := indexer.ProcessPending(ctx)
			if err != nil {
				logger.Error("process pending vectors", "error", err)
			} else if res.Claimed > 0 {
				logger.Info("vector indexing batch", "claimed", res.Claimed,
					"completed", res.Completed, "retried", res.Retried, "failed", res.Failed)
			}
		}
	}
}

package main

import (
	"context"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag"
	"eino-quickstart/internal/rag/constant"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/document"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	cfg := config.Config{
		Knowledge: config.KnowledgeConfig{
			Root:                "./tests/knowledge",
			DefaultTopK:         10,
			ChunkSizeCharacters: 300,
		},

		Embedding: config.EmbeddingConfig{
			BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Model:      "qwen3.7-text-embedding",
			Dimensions: 1024,
		},
		Storage: struct {
			Host        string `yaml:"host"`
			Port        int    `yaml:"port"`
			Username    string `yaml:"username"`
			Password    string `yaml:"-"`
			PasswordEnv string `yaml:"passwordEnv"`
			DBName      string `yaml:"dbName"`
			SSLMode     string `yaml:"sslMode"`
			MaxOpenConn int    `yaml:"maxOpenConn"`
		}{
			Host:        "10.10.2.181",
			Port:        5432,
			Username:    "postgres",
			PasswordEnv: "EINO_STORAGE_PASSWORD",
			DBName:      "eino",
			SSLMode:     "disable",
			MaxOpenConn: 10,
		},
		Milvus: config.MilvusConfig{
			Address:         "10.10.2.90:19530",
			Collection:      "eino_document_chunks_v1",
			MetricType:      "COSINE",
			TopKCandidate:   30,
			SearchTimeoutMS: 3000,
		},
	}

	if cfg.Storage.PasswordEnv != "" {
		cfg.Storage.Password = os.Getenv(cfg.Storage.PasswordEnv)
	}

	emb, err := rag.NewEmbedder(ctx, &openai.EmbeddingConfig{
		APIKey:     os.Getenv("EINO_EMBEDDING_API_KEY"), // [优化] 密钥走环境变量，不进配置文件
		BaseURL:    cfg.Embedding.BaseURL,
		Model:      cfg.Embedding.Model,
		Dimensions: &cfg.Embedding.Dimensions,
	}, rag.WithMaxTextsPerRequest(cfg.Embedding.BatchSize))
	if err != nil {
		slog.Error("init embedder", "err", err)
		os.Exit(1)
	}

	// 生产替换为 milvus/postgres 实现
	entClient, err := entx.Open(ctx, cfg.Storage)
	if err != nil {
		log.Fatal(err)
	}

	// 关键词检索索引（ES）。这条演示链的配置是写死的字面量，所以 ES 段默认为空
	// —— es.New 会返回 nil，检索的词法通道回落到 PostgreSQL 子串匹配。想在这里
	// 试 BM25，把 configs/config.yaml 的 es 段抄到上面的 cfg 里即可。
	//
	// 这里曾经是 es.NewESClient(nil)：它在读 CA 证书时就会空指针 panic，
	// 也就是说这条演示链此前一次都没跑通过。
	searchIndex, err := es.New(&cfg.ES)
	if err != nil {
		log.Fatal(err)
	}

	store, err := rag.NewStore(ctx, entClient, &cfg, searchIndex.Searcher())
	if err != nil {
		log.Fatal(err)
	}

	p, err := rag.NewPipeline(ctx, rag.Config{
		Embedder:       emb,
		Store:          store,
		DocRoot:        cfg.Knowledge.Root,
		ChunkMaxChars:  cfg.Knowledge.ChunkSizeCharacters,
		TopK:           cfg.Knowledge.DefaultTopK,
		ScoreThreshold: 0.5,
	}, entClient)
	if err != nil {
		slog.Error("init pipeline", "err", err)
		os.Exit(1)
	}

	// 示例：入库 + 检索
	if _, err := p.IngestFile(ctx, document.Source{
		URI: cfg.Knowledge.Root + "/H11_二段力滑入式铰链.md",
	}); err != nil {
		slog.Error("ingest", "err", err)
		os.Exit(1)
	}
	hits, err := p.Retrieve(ctx, "H105G")
	if err != nil {
		slog.Error("retrieve", "err", err)
		os.Exit(1)
	}
	for _, h := range hits {
		slog.Info("hit", "score", h.Score, "heading", h.MetaData[constant.MetaHeadingPath], "preview", truncate(h.Content, 80))
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

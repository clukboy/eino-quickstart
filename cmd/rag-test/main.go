package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"eino-quickstart/internal/knowledge/embedding"
	"eino-quickstart/internal/knowledge/retrieval"
	"eino-quickstart/internal/knowledge/vectorstore"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/storage/entx"
)

func main() {
	configPath := os.Getenv("EINO_CONFIG")

	if configPath == "" {
		configPath = "./configs/config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		2*time.Minute,
	)
	defer cancel()

	// ============================================================
	// PostgreSQL
	// ============================================================

	entClient, err := entx.Open(
		ctx,
		cfg.Storage,
	)
	if err != nil {
		log.Fatalf("open postgres: %v", err)
	}
	defer entClient.Close()

	// ============================================================
	// Embedding
	// ============================================================

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
		log.Fatalf(
			"create embedding client: %v",
			err,
		)
	}

	// ============================================================
	// Milvus
	// ============================================================

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
		log.Fatalf(
			"create milvus store: %v",
			err,
		)
	}
	defer vecStore.Close(ctx)

	// ============================================================
	// Searchers
	// ============================================================

	keywordSearcher :=
		retrieval.NewPostgresKeywordSearcher(
			entClient,
		)

	productSearcher :=
		retrieval.NewProductSearcher(
			entClient,
		)

	retriever := &retrieval.HybridRetriever{
		Client:          entClient,
		Embedder:        embedder,
		VectorStore:     vecStore,
		KeywordSearcher: keywordSearcher,
		ProductSearcher: productSearcher,

		DefaultTopK:        cfg.Knowledge.DefaultTopK,
		MaxTopK:            cfg.Knowledge.MaxTopK,
		VectorCandidates:   cfg.Retrieval.VectorCandidateLimit,
		KeywordCandidates:  cfg.Retrieval.KeywordCandidateLimit,
		ExactCandidates:    cfg.Retrieval.ExactCandidateLimit,
		MaxQueryCharacters: cfg.Knowledge.MaxQueryCharacters,
		MaxResultBytes:     cfg.Knowledge.MaxResultBytes,

		VectorWeight:  cfg.Retrieval.VectorWeight,
		KeywordWeight: cfg.Retrieval.KeywordWeight,
		ExactWeight:   cfg.Retrieval.ExactWeight,

		RRFSmoothing: cfg.Retrieval.RRFSmoothing,
	}

	// ============================================================
	// Interactive Query
	// ============================================================

	fmt.Println("==========================================")
	fmt.Println("Product RAG Retrieval Test")
	fmt.Println("==========================================")
	fmt.Println("输入问题进行召回测试。")
	fmt.Println("输入 exit / quit 退出。")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(
		make([]byte, 1024),
		max(64*1024, cfg.Knowledge.MaxQueryCharacters*4+1),
	)
	for {
		fmt.Print("Query > ")

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				log.Printf("read query: %v", err)
			}
			return
		}
		query := strings.TrimSpace(scanner.Text())

		if query == "exit" || query == "quit" {
			return
		}

		debugResult, err := retriever.DebugSearch(
			ctx,
			"system",
			query,
			cfg.Knowledge.DefaultTopK,
		)

		if err != nil {
			fmt.Printf(
				"search failed: %v\n\n",
				err,
			)
			continue
		}

		printDebugResult(debugResult)
	}
}

func printDebugResult(
	result *retrieval.DebugResult,
) {
	fmt.Println()
	fmt.Println("==========================================")
	fmt.Printf("Query: %s\n", result.Query)
	fmt.Println("==========================================")

	fmt.Println()
	fmt.Println("---------- Exact Model ----------")

	for index, item := range result.ExactResults {
		fmt.Printf(
			"%d. chunk=%d score=%.6f\n",
			index+1,
			item.ChunkID,
			item.Score,
		)
	}

	fmt.Println()
	fmt.Println("---------- Keyword ----------")

	for index, item := range result.KeywordResults {
		fmt.Printf(
			"%d. chunk=%d score=%.6f\n",
			index+1,
			item.ChunkID,
			item.Score,
		)
	}

	fmt.Println()
	fmt.Println("---------- Vector ----------")

	for index, item := range result.VectorResults {
		fmt.Printf(
			"%d. chunk=%d score=%.6f\n",
			index+1,
			item.ChunkID,
			item.Score,
		)
	}

	fmt.Println()
	fmt.Println("---------- RRF ----------")

	for index, item := range result.FusedResults {
		fmt.Printf(
			"%d. chunk=%d score=%.6f\n",
			index+1,
			item.ChunkID,
			item.Score,
		)
	}

	fmt.Println()
	fmt.Println("---------- Final Results ----------")

	for index, item := range result.FinalResults {
		fmt.Printf(
			"\n[%d] score=%.6f\n",
			index+1,
			item.Score,
		)

		fmt.Printf(
			"chunk_id: %d\n",
			item.ChunkID,
		)

		fmt.Printf(
			"citation: %s\n",
			item.CitationID,
		)

		fmt.Printf(
			"source: %s\n",
			item.Source,
		)

		fmt.Printf(
			"title: %s\n",
			item.Title,
		)

		if item.HeadingPath != "" {
			fmt.Printf(
				"heading: %s\n",
				item.HeadingPath,
			)
		}

		fmt.Printf(
			"content:\n%s\n",
			item.Content,
		)
	}

	fmt.Println()
	fmt.Println("==========================================")
	fmt.Println()
}

package main

import (
	"context"
	"eino-quickstart/internal/knowledge/retrieval"
	"flag"
	"fmt"
	"os"
	"time"

	"eino-quickstart/internal/knowledge/embedding"
	"eino-quickstart/internal/knowledge/vectorstore"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/storage/entx"
)

func main() {
	query := flag.String("query", "H105 固装", "RAG query")

	topK := flag.Int("top-k", 5, "final result topK")

	vectorTopK := flag.Int("vector-top-k", 20, "vector candidate count")

	keywordTopK := flag.Int("keyword-top-k", 20, "keyword candidate count")

	flag.Parse()

	if *query == "" {
		fmt.Println("query is required")
		fmt.Println()
		fmt.Println("Example:")
		fmt.Println(`  go run ./cmd/rag-test --query "H105 固装"`)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	configPath := os.Getenv("EINO_CONFIG")
	if configPath == "" {
		configPath = "./configs/config.yaml"
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("load config failed: %v\n", err)
		os.Exit(1)
	}

	// PostgreSQL / Ent
	entClient, err := entx.Open(
		ctx,
		cfg.Storage,
	)
	if err != nil {
		fmt.Printf("open storage failed: %v\n", err)
		os.Exit(1)
	}
	defer entClient.Close()

	// Embedding
	embedder, err := embedding.NewOpenAIEmbedder(
		embedding.OpenAIConfig{
			BaseURL: cfg.Embedding.BaseURL,
			APIKey: os.Getenv(
				cfg.Embedding.APIKeyEnv,
			),
			Model:      cfg.Embedding.Model,
			Dimensions: cfg.Embedding.Dimensions,
			BatchSize:  cfg.Embedding.BatchSize,
		},
	)
	if err != nil {
		fmt.Printf("create embedder failed: %v\n", err)
		os.Exit(1)
	}

	// Milvus
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
		fmt.Printf("create vector store failed: %v\n", err)
		os.Exit(1)
	}

	// PostgreSQL Keyword Search
	keywordSearcher := retrieval.NewPostgresKeywordSearcher(
		entClient,
	)

	// Hybrid Retriever
	retriever := &retrieval.HybridRetriever{
		Client:          entClient,
		Embedder:        embedder,
		VectorStore:     vecStore,
		KeywordSearcher: keywordSearcher,

		DefaultTopK:       *topK,
		MaxTopK:           100,
		VectorCandidates:  *vectorTopK,
		KeywordCandidates: *keywordTopK,

		MaxQueryCharacters: 1000,
		MaxResultBytes:     100000,

		VectorWeight:  1.0,
		KeywordWeight: 1.0,

		RRFSmoothing: 60,
	}

	result, err := retriever.DebugSearch(
		ctx,
		"system",
		*query,
		*topK,
	)
	if err != nil {
		fmt.Printf("RAG search failed: %v\n", err)
		os.Exit(1)
	}

	printResult(result)
}

func printResult(result *retrieval.DebugResult) {
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("RAG Retrieval Debug")
	fmt.Println("========================================")

	fmt.Printf("Query: %s\n", result.Query)

	printCandidates(
		"Vector Search",
		result.VectorResults,
	)

	printCandidates(
		"Keyword Search",
		result.KeywordResults,
	)

	printCandidates(
		"RRF Fusion",
		result.FusedResults,
	)

	fmt.Println()
	fmt.Println("========== Final Results ==========")

	for index, item := range result.FinalResults {
		fmt.Println()
		fmt.Printf("[%d]\n", index+1)

		fmt.Printf(
			"ChunkID: %d\n",
			item.ChunkID,
		)

		fmt.Printf(
			"Score: %.8f\n",
			item.Score,
		)

		fmt.Printf(
			"Source: %s\n",
			item.Source,
		)

		fmt.Printf(
			"Title: %s\n",
			item.Title,
		)

		fmt.Printf(
			"Heading: %s\n",
			item.HeadingPath,
		)

		fmt.Printf(
			"Lines: %d-%d\n",
			item.StartLine,
			item.EndLine,
		)

		fmt.Println("Content:")
		fmt.Println("----------------------------------------")
		fmt.Println(item.Content)
		fmt.Println("----------------------------------------")
	}

	fmt.Println()
	fmt.Printf(
		"Final Results: %d\n",
		len(result.FinalResults),
	)
}

func printCandidates(
	title string,
	results []retrieval.Candidate,
) {
	fmt.Println()
	fmt.Printf("========== %s ==========\n", title)

	if len(results) == 0 {
		fmt.Println("(empty)")
		return
	}

	fmt.Printf(
		"%-6s %-12s %-15s\n",
		"Rank",
		"ChunkID",
		"Score",
	)

	for index, item := range results {
		fmt.Printf(
			"%-6d %-12d %-15.8f\n",
			index+1,
			item.ChunkID,
			item.Score,
		)
	}
}

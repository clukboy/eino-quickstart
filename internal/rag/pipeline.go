package rag

import (
	"context"
	"eino-quickstart/ent"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// Config 是 Pipeline 的全部依赖与参数。
type Config struct {
	Embedder       *Embedder
	Reranker       Reranker // 可选
	DocRoot        string
	MaxFileBytes   int64
	ChunkMaxChars  int
	Store          *Store
	TopK           int
	ScoreThreshold float64
	Timeout        time.Duration // [优化] 每次调用的默认超时
}

// Pipeline 封装 ingest / retrieve 两条 eino Chain。
type Pipeline struct {
	ingest   compose.Runnable[document.Source, []*schema.Document]
	retrieve compose.Runnable[string, []*schema.Document]
	timeout  time.Duration
	indexer  *Indexer
	loader   document.Loader
	chunker  *MarkdownChunker
	hybrid   *HybridRetriever
}

func NewPipeline(ctx context.Context, cfg Config, entClient *ent.Client) (*Pipeline, error) {
	loader, err := NewFileLoader(ctx, FileLoaderConfig{
		Root:     cfg.DocRoot,
		MaxBytes: cfg.MaxFileBytes,
	})
	if err != nil {
		return nil, err
	}
	chunker := NewMarkdownChunker(cfg.ChunkMaxChars)
	indexer := NewIndexer(cfg.Embedder, cfg.Store)
	hybrid, err := NewHybridRetriever(HybridConfig{
		Store: cfg.Store, Embedder: cfg.Embedder, Reranker: cfg.Reranker,
		TopK: cfg.TopK, ScoreThreshold: cfg.ScoreThreshold,
	})
	if err != nil {
		return nil, err
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}

	// ---- ingest chain: uri -> load -> chunk -> index ----
	ingestChain := compose.NewChain[document.Source, []*schema.Document]().
		AppendLoader(loader).
		AppendDocumentTransformer(chunker).
		AppendLambda(compose.InvokableLambda(indexer.Index))

	ingestRun, err := ingestChain.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("rag: compile ingest chain: %w", err)
	}

	// ---- retrieve chain: query -> hybrid retrieve ----
	retChain := compose.NewChain[string, []*schema.Document]().
		AppendRetriever(hybrid)

	retRun, err := retChain.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("rag: compile retrieve chain: %w", err)
	}

	return &Pipeline{
		ingest: ingestRun, retrieve: retRun, timeout: cfg.Timeout,
		indexer: indexer, loader: loader, chunker: chunker, hybrid: hybrid,
	}, nil
}

// IngestFile 入库单个文件（uri 必须在 DocRoot 内）。
func (p *Pipeline) IngestFile(ctx context.Context, uri document.Source) ([]*schema.Document, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	docs, err := p.ingest.Invoke(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("rag: ingest %q: %w", uri, err)
	}
	return docs, nil
}

// Retrieve 检索，返回按相关性排序的 chunk。
func (p *Pipeline) Retrieve(ctx context.Context, query string, opts ...compose.Option) ([]*schema.Document, error) {

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	docs, err := p.retrieve.Invoke(ctx, query, opts...)
	if err != nil {
		return nil, fmt.Errorf("rag: retrieve: %w", err)
	}
	return docs, nil
}

// 保证接口兼容的编译期断言
var (
	_ document.Loader      = (*FileLoader)(nil)
	_ document.Transformer = (*MarkdownChunker)(nil)
	_ retriever.Retriever  = (*HybridRetriever)(nil)
)

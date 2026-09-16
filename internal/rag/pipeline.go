package rag

import (
	"context"
	"eino-quickstart/ent"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// Config 是 Pipeline 的全部依赖与参数。
type Config struct {
	Embedder      *Embedder
	Reranker      Reranker // 可选
	DocRoot       string
	MaxFileBytes  int64
	ChunkMaxChars int

	// Store 是索引与检索的存储尾段。nil 表示这条链只做 load -> parse -> chunk：
	// ingest 链在切块之后结束，IngestFile 直接返回分块结果，Retrieve 不可用。
	//
	// 知识库索引走的就是这一档：分块要落到 document_chunks 上并带着 pending/
	// indexed 状态被逐轮推进，那是应用层的职责，所以 worker 只借这条链的
	// 「读入 -> 拆分 -> 切块」，落库与状态机由它自己写。
	Store *Store

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
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}

	// ---- ingest chain: uri -> load -> chunk ----
	// 尾段的 embed + 落库只在注入了 Store 时才挂上；没注入时这条链的产物就是
	// 分块本身，调用方自己决定怎么存。
	ingestChain := compose.NewChain[document.Source, []*schema.Document]().
		AppendLoader(loader).
		AppendDocumentTransformer(chunker)

	p := &Pipeline{
		timeout: cfg.Timeout,
		loader:  loader,
		chunker: chunker,
	}

	if cfg.Store != nil {
		indexer := NewIndexer(cfg.Embedder, cfg.Store)
		hybrid, err := NewHybridRetriever(HybridConfig{
			Store: cfg.Store, Embedder: cfg.Embedder, Reranker: cfg.Reranker,
			TopK: cfg.TopK, ScoreThreshold: cfg.ScoreThreshold,
		})
		if err != nil {
			return nil, err
		}

		ingestChain = ingestChain.AppendLambda(compose.InvokableLambda(indexer.Index))

		// ---- retrieve chain: query -> hybrid retrieve ----
		retRun, err := compose.NewChain[string, []*schema.Document]().
			AppendRetriever(hybrid).
			Compile(ctx)
		if err != nil {
			return nil, fmt.Errorf("rag: compile retrieve chain: %w", err)
		}

		p.indexer = indexer
		p.hybrid = hybrid
		p.retrieve = retRun
	}

	ingestRun, err := ingestChain.Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("rag: compile ingest chain: %w", err)
	}
	p.ingest = ingestRun
	return p, nil
}

// IngestFile 入库单个文件（uri 必须在 DocRoot 内）。
//
// 注入了 Store 时返回的是已写入存储的分块；没有注入 Store（Store == nil）时
// 链尾就是切块，返回的是「读入 -> 拆分 -> 切块」之后的分块，尚未落库。
func (p *Pipeline) IngestFile(ctx context.Context, uri document.Source, opts ...compose.Option) ([]*schema.Document, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	docs, err := p.ingest.Invoke(ctx, uri, opts...)
	if err != nil {
		return nil, fmt.Errorf("rag: ingest %q: %w", uri.URI, err)
	}
	return docs, nil
}

// Retrieve 检索，返回按相关性排序的 chunk。
func (p *Pipeline) Retrieve(ctx context.Context, query string, opts ...compose.Option) ([]*schema.Document, error) {
	if p.retrieve == nil {
		return nil, errors.New("rag: retrieve unavailable: pipeline was built without a Store")
	}
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

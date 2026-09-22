package rag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"eino-quickstart/ent"

	"github.com/cloudwego/eino/components/document"
	einoparser "github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// Config 是 Pipeline 的全部依赖与参数。
type Config struct {
	Embedder      *Embedder
	Reranker      Reranker // 可选
	MaxFileBytes  int64
	ChunkMaxChars int

	// DocRoot 是「从磁盘读入」那条链的根目录。**可以为空**。
	//
	// 知识库那条路已经不需要它了：正文存在 documents.content 里，索引时直接
	// 读内存（见 IngestContent）。留着它是给 cmd/ragserver 那种「拿一个目录里
	// 的 Markdown 试一下检索」的用法。空值时 IngestFile 不可用，其余能力照常。
	DocRoot string

	// Store 是索引与检索的存储尾段。nil 表示这条链只做 load -> parse -> chunk：
	// ingest 链在切块之后结束，IngestFile 直接返回分块结果，Retrieve 不可用。
	//
	// 知识库索引走的就是这一档：分块要落到 document_chunks 上并带着 pending/
	// indexed 状态被逐轮推进，那是应用层的职责，所以 worker 只借这条链的
	// 「读入 -> 拆分 -> 切块」，落库与状态机由它自己写。
	Store *Store

	TopK           int
	ScoreThreshold float64
	// Retrieval 是三条通道的权重、融合平滑与候选上限（配置的 retrieval 段）。
	// 零值表示用内置默认策略。
	Retrieval RetrievalPolicy
	Timeout   time.Duration // [优化] 每次调用的默认超时
}

// ErrIngestFileUnavailable 表示这条 Pipeline 没有配 DocRoot，读不了文件。
//
// 它是**装配**问题而不是数据问题：调用方该改用 IngestContent（正文在内存里）
// 或者给组合根补上 DocRoot。
var ErrIngestFileUnavailable = errors.New("rag: file ingestion needs a doc root, but the pipeline was built without one")

// Pipeline 封装 ingest / retrieve 两条 eino Chain。
type Pipeline struct {
	// ingest 是「文件 -> 读入 -> 拆分 -> 切块」。DocRoot 为空时是 nil。
	ingest   compose.Runnable[document.Source, []*schema.Document]
	retrieve compose.Runnable[string, []*schema.Document]
	timeout  time.Duration
	indexer  *Indexer
	chunker  *MarkdownChunker
	hybrid   *HybridRetriever
	// text 是正文已在内存里时用的解析器。它不做任何按内容分派 —— 见 IngestContent。
	text einoparser.Parser
}

func NewPipeline(ctx context.Context, cfg Config, entClient *ent.Client) (*Pipeline, error) {
	chunker := NewMarkdownChunker(cfg.ChunkMaxChars)
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}

	p := &Pipeline{
		timeout: cfg.Timeout,
		chunker: chunker,
		text:    einoparser.TextParser{},
	}

	// ---- retrieve chain: query -> hybrid retrieve ----
	if cfg.Store != nil {
		p.indexer = NewIndexer(cfg.Embedder, cfg.Store)
		hybrid, err := NewHybridRetriever(HybridConfig{
			Store: cfg.Store, Embedder: cfg.Embedder, Reranker: cfg.Reranker,
			TopK: cfg.TopK, ScoreThreshold: cfg.ScoreThreshold,
			Retrieval: cfg.Retrieval,
		})
		if err != nil {
			return nil, err
		}
		retRun, err := compose.NewChain[string, []*schema.Document]().
			AppendRetriever(hybrid).
			Compile(ctx)
		if err != nil {
			return nil, fmt.Errorf("rag: compile retrieve chain: %w", err)
		}
		p.hybrid = hybrid
		p.retrieve = retRun
	}

	// ---- ingest chain: uri -> load -> chunk ----
	// 只有配了 DocRoot 才建这条链。空值时整条链不存在，而不是「存在但读不到
	// 东西」—— 后者的失败会伪装成「文件不存在」，把装配问题说成数据问题。
	//
	// 尾段的 embed + 落库只在注入了 Store 时才挂上；没注入时这条链的产物就是
	// 分块本身，调用方自己决定怎么存。
	if strings.TrimSpace(cfg.DocRoot) != "" {
		loader, err := NewFileLoader(ctx, FileLoaderConfig{
			Root:     cfg.DocRoot,
			MaxBytes: cfg.MaxFileBytes,
		})
		if err != nil {
			return nil, err
		}
		ingestChain := compose.NewChain[document.Source, []*schema.Document]().
			AppendLoader(loader).
			AppendDocumentTransformer(chunker)
		if p.indexer != nil {
			ingestChain = ingestChain.AppendLambda(compose.InvokableLambda(p.indexer.Index))
		}
		ingestRun, err := ingestChain.Compile(ctx)
		if err != nil {
			return nil, fmt.Errorf("rag: compile ingest chain: %w", err)
		}
		p.ingest = ingestRun
	}

	return p, nil
}

// IngestFile 入库单个文件（uri 必须在 DocRoot 内）。
//
// 注入了 Store 时返回的是已写入存储的分块；没有注入 Store（Store == nil）时
// 链尾就是切块，返回的是「读入 -> 拆分 -> 切块」之后的分块，尚未落库。
func (p *Pipeline) IngestFile(ctx context.Context, uri document.Source, opts ...compose.Option) ([]*schema.Document, error) {
	if p.ingest == nil {
		return nil, ErrIngestFileUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	docs, err := p.ingest.Invoke(ctx, uri, opts...)
	if err != nil {
		return nil, fmt.Errorf("rag: ingest %q: %w", uri.URI, err)
	}
	return docs, nil
}

// IngestContent 用**内存里的正文**跑「解析 -> 切块」，不碰文件系统。
//
// 这是知识库索引用的一条路：正文存在 documents.content 上，本来就只有一个块
// （一篇文档），按产品拆分早在写入请求里做完了。把它再写回磁盘、再从磁盘读
// 回来解析一遍，换来的只是「盘上的副本和库里的正文可能不一致」。
//
// 这里刻意用 TextParser 而不是按数据集类型分派：分派是给**文件**用的 ——
// 一份型录文件里可能有好几个产品块，要靠 ProductParser 拆开。而到了这一步
// 正文已经是一篇文档，再拆一次只会拆出 0 块（YAML 头已经进了 metadata，
// 正文里没有围栏），然后以「切不出任何分块」的名义把文档判死。
//
// source 只作溯源用（写进 _source 元数据），不参与任何文件操作。
func (p *Pipeline) IngestContent(ctx context.Context, source, content string) ([]*schema.Document, error) {
	if strings.TrimSpace(content) == "" {
		return nil, errors.New("rag: cannot ingest empty content")
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	parsed, err := p.text.Parse(ctx, strings.NewReader(content), einoparser.WithURI(source))
	if err != nil {
		return nil, fmt.Errorf("rag: parse content of %q: %w", source, err)
	}
	chunks, err := p.chunker.Transform(ctx, parsed)
	if err != nil {
		return nil, fmt.Errorf("rag: chunk content of %q: %w", source, err)
	}
	return chunks, nil
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

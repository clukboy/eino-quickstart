package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/rag"
	"eino-quickstart/internal/rag/constant"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

// fakeRetriever 记录检索器收到的查询与选项。
//
// 选项用 eino 导出的 GetCommonOptions 展开成结构体，这样「TopK 传了没有」
// 「范围条件在不在」这两件事可以被直接断言 —— 它们是适配器最容易丢的东西，
// 而丢掉任何一条都不会报错：TopK 丢了退回默认条数，范围丢了变成全库检索
// （那是越权，不只是结果不准）。
type fakeRetriever struct {
	calls  int
	query  string
	opts   *retriever.Options
	docs   []*schema.Document
	report rag.Report
	err    error
}

func (f *fakeRetriever) RetrieveDetailed(
	_ context.Context,
	query string,
	opts ...retriever.Option,
) ([]*schema.Document, rag.Report, error) {
	f.calls++
	f.query = query
	f.opts = retriever.GetCommonOptions(&retriever.Options{}, opts...)
	if f.err != nil {
		return nil, rag.Report{}, f.err
	}
	return f.docs, f.report, nil
}

// scopeFilter 从检索选项里取出适配器翻出来的数据集范围。
//
// 不按 key 取：那个 key 是 rag 包内部的常量，从这里看不见。按类型找反而更贴近
// 它要守的性质 —— 「这次检索到底有没有带范围」，而不是「带的范围叫什么名字」。
func scopeFilter(t *testing.T, opts *retriever.Options) rag.Filter {
	t.Helper()

	if opts == nil {
		t.Fatal("检索器没有收到任何选项")
	}
	for _, value := range opts.DSLInfo {
		if filter, ok := value.(rag.Filter); ok {
			return filter
		}
	}
	t.Fatalf("选项里没有数据集范围，检索会退化成全库范围：%+v", opts.DSLInfo)
	return rag.Filter{}
}

// 适配器必须把范围与条数原样交给检索器。
//
// 这两条一起守的是「调用方能搜到什么」：范围丢了下游会跨数据集召回，条数丢了
// 响应条数会与请求不符。两者都不会报错，只看结果是看不出来的。
func TestHybridSearcherPassesScopeToRetriever(t *testing.T) {
	fake := &fakeRetriever{}
	searcher := hybridSearcher{retriever: fake}

	if _, err := searcher.Search(context.Background(), "H105P", knowledge.SearchScope{
		DatasetID: 42,
		TopK:      7,
	}); err != nil {
		t.Fatalf("检索失败: %v", err)
	}

	if fake.calls != 1 {
		t.Fatalf("检索器被调用 %d 次，期望 1 次", fake.calls)
	}
	if fake.query != "H105P" {
		t.Errorf("查询原样透传，实际 %q", fake.query)
	}

	if got := scopeFilter(t, fake.opts).DatasetID; got != 42 {
		t.Errorf("数据集范围透传为 %d，期望 42", got)
	}
	if fake.opts.TopK == nil {
		t.Fatal("TopK 没有传下去，检索会退回默认条数")
	}
	if *fake.opts.TopK != 7 {
		t.Errorf("TopK 透传为 %d，期望 7", *fake.opts.TopK)
	}
}

// 结果要摊平成用例层的形状，且通道台账必须带上去。
//
// 台账（channels / degraded）是「结果为什么这么少」唯一的现场证据：压在适配器里
// 就只剩日志能看了，而调用方恰好是最需要拿它区分「通道挂了」和「没有相关内容」的人。
func TestHybridSearcherFlattensHitsAndKeepsLedger(t *testing.T) {
	doc := (&schema.Document{
		ID:      "123",
		Content: "铰链型号 H105P 的规格。",
		MetaData: map[string]any{
			constant.MetaDocID:       uint64(77),
			constant.MetaSource:      "documents/9/h105p.md",
			constant.MetaTitle:       "H105P 型录",
			constant.MetaHeadingPath: "规格 > 铰链",
		},
	}).WithScore(0.87)

	fake := &fakeRetriever{
		docs: []*schema.Document{doc},
		report: rag.Report{Channels: []rag.ChannelState{
			{Name: string(rag.ChannelExact), Candidates: 1},
			{Name: string(rag.ChannelKeyword), Candidates: 3},
			// 向量通道没给出候选、也没有错误：既不算「出力」也不算「降级」。
			{Name: string(rag.ChannelVector)},
		}},
	}
	searcher := hybridSearcher{retriever: fake}

	outcome, err := searcher.Search(context.Background(), "H105P", knowledge.SearchScope{
		DatasetID: 9,
		TopK:      5,
	})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}

	if len(outcome.Hits) != 1 {
		t.Fatalf("命中 %d 条，期望 1 条", len(outcome.Hits))
	}
	hit := outcome.Hits[0]
	if hit.ChunkID != 123 {
		t.Errorf("chunk_id 解析为 %d，期望 123", hit.ChunkID)
	}
	if hit.DocumentID != 77 {
		t.Errorf("document_id 取到 %d，期望 77", hit.DocumentID)
	}
	if hit.Source != "documents/9/h105p.md" {
		t.Errorf("source 取到 %q", hit.Source)
	}
	if hit.Title != "H105P 型录" {
		t.Errorf("title 取到 %q", hit.Title)
	}
	if hit.HeadingPath != "规格 > 铰链" {
		t.Errorf("heading_path 取到 %q", hit.HeadingPath)
	}
	if hit.Content != doc.Content {
		t.Errorf("content 取到 %q", hit.Content)
	}
	if hit.Score != 0.87 {
		t.Errorf("score 取到 %v，期望 0.87", hit.Score)
	}

	if len(outcome.Channels) != 2 {
		t.Errorf("出力通道应当只有给出候选的两条，实际 %v", outcome.Channels)
	}
	if len(outcome.Degraded) != 0 {
		t.Errorf("没有通道报错时不该有降级项，实际 %v", outcome.Degraded)
	}
}

// 通道降级要如实上报，而不是被吞掉。
//
// 吞掉之后，「向量库挂了」在调用方眼里与「确实没有相关内容」完全一样。
func TestHybridSearcherReportsDegradedChannels(t *testing.T) {
	fake := &fakeRetriever{
		report: rag.Report{Channels: []rag.ChannelState{
			{Name: string(rag.ChannelExact), Candidates: 2},
			{Name: string(rag.ChannelVector), Err: "milvus: connection refused"},
		}},
	}
	searcher := hybridSearcher{retriever: fake}

	outcome, err := searcher.Search(context.Background(), "图冠系列", knowledge.SearchScope{DatasetID: 1, TopK: 3})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}

	if len(outcome.Degraded) != 1 || outcome.Degraded[0] != string(rag.ChannelVector) {
		t.Errorf("降级通道应当上报 vector，实际 %v", outcome.Degraded)
	}
	// 空结果也要是可编码的空数组：nil 走出去会变成 JSON null，调用方得为
	// 「没有命中」和「字段缺失」写两套分支。
	if outcome.Hits == nil {
		t.Error("没有命中时应当是空数组而不是 nil")
	}
	if len(outcome.Hits) != 0 {
		t.Errorf("期望 0 条命中，实际 %d 条", len(outcome.Hits))
	}
}

// 检索器的错误要原样往上抛，不能在适配器里被降级成空结果。
func TestHybridSearcherPropagatesRetrieverError(t *testing.T) {
	wantErr := errors.New("milvus: unavailable")
	fake := &fakeRetriever{err: wantErr}
	searcher := hybridSearcher{retriever: fake}

	_, err := searcher.Search(context.Background(), "H105P", knowledge.SearchScope{DatasetID: 1, TopK: 5})
	if !errors.Is(err, wantErr) {
		t.Fatalf("期望原样返回检索错误，实际 %v", err)
	}
}

// 元数据里的 document_id 要认多种类型。
//
// 它有两条来路：检索侧重建元数据时写进去的是 uint64，而经过 JSON 列往返的可能
// 变成 float64 或 string。少认一种的表现是 document_id 静默变成 0，而 0 在客户端
// 看起来只是「这个字段没填」，不会有人去查。
func TestMetaUint64AcceptsJSONRoundTrippedTypes(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  uint64
	}{
		{name: "uint64", value: uint64(77), want: 77},
		{name: "int64", value: int64(77), want: 77},
		{name: "float64", value: float64(77), want: 77},
		{name: "string", value: "77", want: 77},
		{name: "负数当没有", value: int64(-1), want: 0},
		{name: "解析不了当没有", value: "abc", want: 0},
		{name: "意料之外的类型当没有", value: true, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &schema.Document{MetaData: map[string]any{constant.MetaDocID: tc.value}}
			if got := metaUint64(doc, constant.MetaDocID); got != tc.want {
				t.Errorf("解析为 %d，期望 %d", got, tc.want)
			}
		})
	}

	if got := metaUint64(nil, constant.MetaDocID); got != 0 {
		t.Errorf("文档为 nil 时应当返回 0，实际 %d", got)
	}
}

// 没有数据库连接时，装配必须明确报告「这里没有召回能力」而不是给一个半残的检索器。
//
// 返回 nil 会让用例层给出 ErrSearchUnavailable、传输层映射成 503 —— 运维一眼知道
// 是装配问题。给一个每次查询都失败的检索器，表现就是「接口能用但永远搜不到」。
func TestNewRetrievalSearcherWithoutDatabaseIsUnavailable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	searcher := newRetrievalSearcher(context.Background(), &config.Config{}, nil, logger)
	if searcher != nil {
		t.Fatalf("没有数据库连接时应当返回 nil，实际 %T", searcher)
	}
}

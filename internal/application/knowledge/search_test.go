package knowledge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"eino-quickstart/ent"
	"eino-quickstart/internal/rag/grouping"
)

// fakeSearcher 是召回端口的桩：它只记录「被调用了没有、收到什么范围」。
type fakeSearcher struct {
	calls int
	scope SearchScope
	query string
	out   SearchOutcome
	err   error
}

func (f *fakeSearcher) Search(_ context.Context, query string, scope SearchScope) (SearchOutcome, error) {
	f.calls++
	f.query = query
	f.scope = scope
	if f.err != nil {
		return SearchOutcome{}, f.err
	}
	return f.out, nil
}

func testLimits() Limits {
	return Limits{DefaultTopK: 5, MaxTopK: 10, MaxQueryCharacters: 20}.withDefaults()
}

// TestSearchWithoutSearcherIsUnavailable 钉住「没装配检索」和「搜不到」的区别。
//
// 前者是部署形态问题（重试无用，运维该看装配），后者是内容问题（换个说法再问）。
// 混成一个空结果，调用方会一直重试一个永远不可能成功的请求。
func TestSearchWithoutSearcherIsUnavailable(t *testing.T) {
	service := &Service{limits: testLimits()}

	_, err := service.Search(context.Background(), SearchInput{DatasetID: 1, Query: "H105P"})
	if !errors.Is(err, ErrSearchUnavailable) {
		t.Fatalf("期望 ErrSearchUnavailable，实际 %v", err)
	}
}

// TestSearchValidatesBeforeTouchingDependencies 钉住校验的前置性。
//
// 参数不对必须在查询数据库与检索之前就拒绝：否则一个空查询会先花掉一次数据集
// 往返，再走到检索侧被 ES 当成 match_all（那会把整库前几条捞回来当结果）。
func TestSearchValidatesBeforeTouchingDependencies(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{name: "空查询", query: "   "},
		{name: "超长查询", query: strings.Repeat("铰", 21)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			searcher := &fakeSearcher{}
			service := &Service{searcher: searcher, limits: testLimits()}

			_, err := service.Search(context.Background(), SearchInput{DatasetID: 1, Query: tc.query})
			var invalidErr *ValidationError
			if !errors.As(err, &invalidErr) {
				t.Fatalf("期望校验错误，实际 %v", err)
			}
			if searcher.calls != 0 {
				t.Error("校验失败时不该发出检索请求")
			}
		})
	}
}

// TestLimitsClampTopK 钉住条数的收敛区间。
//
// 不收敛的表现是「传 top_k=1000 真的取回 1000 条」—— 一次请求就能把提示词
// 预算和数据库往返打满，而这是调用方随手就能做到的事。
func TestLimitsClampTopK(t *testing.T) {
	limits := testLimits()

	cases := []struct {
		name      string
		requested int
		want      int
	}{
		{name: "不传用默认值", requested: 0, want: 5},
		{name: "负数当不传", requested: -3, want: 5},
		{name: "区间内原样", requested: 7, want: 7},
		{name: "超过上限压到上限", requested: 1000, want: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := limits.clampTopK(tc.requested); got != tc.want {
				t.Errorf("topK 收敛为 %d，期望 %d", got, tc.want)
			}
		})
	}
}

// TestLimitsDefaultsAreSelfConsistent 钉住配置写反时的兜底。
//
// 默认值大于上限（默认 10、上限 5）时如果不收敛，表现是「不传 top_k 比传 top_k
// 拿到的还多」—— 一个自相矛盾的接口，而且不会有任何报错。
func TestLimitsDefaultsAreSelfConsistent(t *testing.T) {
	limits := Limits{DefaultTopK: 10, MaxTopK: 5}.withDefaults()
	if limits.DefaultTopK > limits.MaxTopK {
		t.Fatalf("默认值 %d 超过了上限 %d", limits.DefaultTopK, limits.MaxTopK)
	}
	if got := limits.clampTopK(0); got != limits.MaxTopK {
		t.Errorf("不传时应当拿 %d，实际 %d", limits.MaxTopK, got)
	}
}

// TestLimitsZeroValueIsUsable 钉住零值 Limits 也能用。
//
// 组合根忘了传限制时，正确的表现是「用一套保守的默认值」，而不是「任何查询都
// 超长、任何条数都超限」—— 后者会让检索接口在配错的那一刻整体不可用。
func TestLimitsZeroValueIsUsable(t *testing.T) {
	limits := Limits{}.withDefaults()
	if limits.DefaultTopK <= 0 || limits.MaxTopK < limits.DefaultTopK || limits.MaxQueryCharacters <= 0 {
		t.Fatalf("零值没有补出可用的默认值: %+v", limits)
	}
}

func searchHit(source string, score float64) SearchHit {
	return SearchHit{Source: source, Score: score, Content: source}
}

// 按文档归并时同一篇文档只留一条，取得分最高的那一块作代表。
//
// 不归并的表现是：产品型录里同一个产品占满整个结果列表 —— 调用方拿到的「五条
// 结果」其实是一篇文档的五个段落，而那看起来像召回质量差。
func TestGroupHitsByDocumentKeepsBestChunk(t *testing.T) {
	hits := []SearchHit{
		{Source: "documents/2/H105P.md", Score: 0.4, Content: "低分块"},
		{Source: "documents/2/H105P.md", Score: 0.9, Content: "高分块"},
		{Source: "documents/2/其他.md", Score: 0.7, Content: "另一篇"},
		{Source: "documents/2/H105P.md", Score: 0.6, Content: "中分块"},
	}

	merged := groupHits(hits, grouping.Document)

	if len(merged) != 2 {
		t.Fatalf("应归并成 2 条，实际 %d: %+v", len(merged), merged)
	}
	if merged[0].Source != "documents/2/H105P.md" || merged[0].Content != "高分块" {
		t.Fatalf("第一条应是该文档的最高分块，实际 %+v", merged[0])
	}
	if merged[1].Source != "documents/2/其他.md" {
		t.Fatalf("第二条应是另一篇，实际 %+v", merged[1])
	}
}

// 归并不重排。顺序是检索侧（融合与 rerank）的结论，在这里按分数再排一遍，
// 「权重改了有没有生效」就再也看不出来了 —— 分数是融合后的值，不反映原名次。
func TestGroupHitsKeepsRetrievalOrder(t *testing.T) {
	hits := []SearchHit{
		{Source: "documents/2/a.md", Score: 0.50},
		{Source: "documents/2/b.md", Score: 0.95},
		{Source: "documents/2/a.md", Score: 0.90},
	}

	merged := groupHits(hits, grouping.Document)

	if len(merged) != 2 || merged[0].Source != "documents/2/a.md" || merged[1].Source != "documents/2/b.md" {
		t.Fatalf("归并后应保持检索给出的顺序，实际 %+v", merged)
	}
	// 代表块仍取最高分：名次不变，内容取最好的那一块。
	if merged[0].Score != 0.90 {
		t.Fatalf("代表块应取 A 的最高分 0.90，实际 %v", merged[0].Score)
	}
}

// chunk 粒度下原样返回：普通文档库里一篇长文切成几百块，归并成一条等于什么
// 都没返回，而调用方还以为「内容只有这么点」。
func TestGroupHitsByChunkIsIdentity(t *testing.T) {
	hits := []SearchHit{
		{Source: "documents/2/长文.md", Score: 0.9, ChunkID: 1},
		{Source: "documents/2/长文.md", Score: 0.8, ChunkID: 2},
	}

	merged := groupHits(hits, grouping.Chunk)

	if len(merged) != 2 {
		t.Fatalf("chunk 粒度不该归并，实际 %d 条", len(merged))
	}
	if merged[1].ChunkID != 2 {
		t.Fatalf("chunk 粒度下每条要保留自己的分块身份，实际 %+v", merged[1])
	}
}

// source 去空白后比较：检索实现可能在元数据里带空格，那不该让同一篇文档
// 分裂成两条。
func TestGroupHitsTrimsSourceWhitespace(t *testing.T) {
	hits := []SearchHit{
		{Source: " documents/2/a.md", Score: 0.9},
		{Source: "documents/2/a.md ", Score: 0.8},
	}

	merged := groupHits(hits, grouping.Document)

	if len(merged) != 1 {
		t.Fatalf("同一篇文档（含空白差异）应归并成 1 条，实际 %d", len(merged))
	}
}

// 空结果与单条结果直接返回，不额外分配 —— 归并的语义在这两种情况下本来就是恒等。
func TestGroupHitsHandlesDegenerateInput(t *testing.T) {
	if merged := groupHits(nil, grouping.Document); len(merged) != 0 {
		t.Fatalf("空输入应返回空，实际 %d 条", len(merged))
	}
	single := []SearchHit{{Source: "documents/2/a.md", Score: 1}}
	if merged := groupHits(single, grouping.Document); len(merged) != 1 {
		t.Fatalf("单条输入应原样返回，实际 %d 条", len(merged))
	}
}

// 粒度必须取自**数据集自己的 type**，不是某个写死的值或请求参数。
//
// 拿错来源不会报错：只会让某一类库的结果条数悄悄换成另一种单位。所以这里用两个
// 类型的数据集跑同一批命中，断言按各自粒度归并出来的条数确实不同。
func TestGranularityResolvesFromDatasetType(t *testing.T) {
	policy, err := grouping.NewPolicy(map[string]string{
		grouping.DefaultKey: "chunk",
		"product":           "document",
	})
	if err != nil {
		t.Fatalf("构造策略失败: %v", err)
	}
	service := &Service{grouping: policy}

	hits := []SearchHit{
		{Source: "documents/2/H105P.md", Score: 0.9},
		{Source: "documents/2/H105P.md", Score: 0.7},
	}

	productGranularity := service.granularityFor(&ent.Dataset{Type: "product"})
	if productGranularity != grouping.Document {
		t.Fatalf("product 库应归并到文档，实际 %q", productGranularity)
	}
	if merged := groupHits(hits, productGranularity); len(merged) != 1 {
		t.Fatalf("product 库两块的同一篇文档应归并成 1 条，实际 %d", len(merged))
	}

	plainGranularity := service.granularityFor(&ent.Dataset{Type: "text"})
	if plainGranularity != grouping.Chunk {
		t.Fatalf("text 库应保持分块粒度，实际 %q", plainGranularity)
	}
	if merged := groupHits(hits, plainGranularity); len(merged) != 2 {
		t.Fatalf("text 库按分块返回，期望 2 条，实际 %d", len(merged))
	}
}

// 数据集没有 type（默认空串）时走兜底粒度，不能报错也不能丢结果。
func TestGranularityFallsBackWhenTypeIsEmpty(t *testing.T) {
	service := &Service{}

	granularity := service.granularityFor(&ent.Dataset{})

	if granularity != grouping.Default {
		t.Fatalf("空类型应取默认粒度 %q，实际 %q", grouping.Default, granularity)
	}
	if got := groupHits([]SearchHit{{Source: "documents/2/a.md", Score: 1}}, granularity); len(got) != 1 {
		t.Fatalf("命中的 1 条不该丢，实际 %d", len(got))
	}
}

// poolSearcher 是一个「候选池有限」的检索桩：它按预算从固定池子里截取候选。
//
// 之所以不写成「给多少预算就返回多少条」，是因为取数预算的关键性质正好在截断与
// 见底上：检索实现只会给到候选池的边界，给不满预算**就是**「库里就这么多」的信号。
// 一个永远不会见底的桩会让加码循环看起来永远正确。
type poolSearcher struct {
	pool    []SearchHit
	budgets []int
	err     error
}

func (s *poolSearcher) Search(_ context.Context, _ string, scope SearchScope) (SearchOutcome, error) {
	if s.err != nil {
		return SearchOutcome{}, s.err
	}
	s.budgets = append(s.budgets, scope.ChunkBudget)
	pool := append([]SearchHit(nil), s.pool...)
	if scope.ChunkBudget > 0 && len(pool) > scope.ChunkBudget {
		pool = pool[:scope.ChunkBudget]
	}
	return SearchOutcome{Hits: pool}, nil
}

// productPool 造一个「每篇文档切成 chunksPerDoc 块」的候选池，模拟产品型录的
// 切块密度。同一篇文档的块在名次上连在一起，正是「20 块只覆盖 4 篇」的场景。
func productPool(documents, chunksPerDoc int) []SearchHit {
	hits := make([]SearchHit, 0, documents*chunksPerDoc)
	for doc := 1; doc <= documents; doc++ {
		for chunk := 0; chunk < chunksPerDoc; chunk++ {
			hits = append(hits, SearchHit{
				Source:  fmt.Sprintf("documents/9/p%03d.md", doc),
				Content: fmt.Sprintf("块 %d", chunk),
				Score:   float64(chunksPerDoc-chunk) / 10,
			})
		}
	}
	return hits
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestService 建一个只装了召回依赖的 Service。
//
// 不走 NewService：那里要求 ent 客户端与索引队列，而这里要测的是取数与正文补全
// 这两段纯逻辑。为了它们搭一个数据库，代价是这段逻辑最需要被测的部分反而没人跑。
func newTestService(searcher Searcher) *Service {
	return &Service{
		searcher: searcher,
		limits:   testLimits(),
		logger:   discardLogger(),
	}
}

// 要 20 篇文档时，预算不够就必须加码再取。
//
// 这是「请求 20 篇、返回 4 篇」的直接修复：检索侧只认识分块，20 块在每篇切 5 块
// 的型录库上只覆盖 4 篇。断言的是**最终条数取满**，而不只是「多跑了一轮」——
// 后者在预算算错时同样成立。
func TestRecallWidensBudgetUntilDocumentsFill(t *testing.T) {
	searcher := &poolSearcher{pool: productPool(20, 5)} // 100 块 / 20 篇
	service := newTestService(searcher)

	outcome, err := service.recall(context.Background(), "固装铰链", 9, grouping.Document, 20)
	if err != nil {
		t.Fatalf("取数失败: %v", err)
	}

	if len(outcome.Hits) != 20 {
		t.Fatalf("应取满 20 篇，实际 %d 篇（预算序列 %v）", len(outcome.Hits), searcher.budgets)
	}
	if len(searcher.budgets) != 2 || searcher.budgets[0] != 80 || searcher.budgets[1] != 160 {
		t.Fatalf("应先按 80 块取、再翻到 160 块，实际预算序列 %v", searcher.budgets)
	}
	if outcome.ChunkBudget != 160 {
		t.Fatalf("台账里的预算应为最后一轮的 160，实际 %d", outcome.ChunkBudget)
	}
	if outcome.MatchedChunks != 100 {
		t.Fatalf("命中块数应为池子里的 100，实际 %d", outcome.MatchedChunks)
	}
	if outcome.Granularity != grouping.Document {
		t.Fatalf("粒度台账不对: %q", outcome.Granularity)
	}
}

// 库里就这么多内容时必须立刻收手，不能一路翻倍到上限。
//
// 少了这条刹车，一次「库里只有 4 篇能匹配」的召回会白跑几轮三通道检索：延迟翻几倍，
// 结果一模一样，而日志上看起来像检索在正常工作。
func TestRecallStopsWhenRetrieverIsExhausted(t *testing.T) {
	searcher := &poolSearcher{pool: productPool(4, 5)} // 20 块 / 4 篇
	service := newTestService(searcher)

	outcome, err := service.recall(context.Background(), "固装铰链", 9, grouping.Document, 20)
	if err != nil {
		t.Fatalf("取数失败: %v", err)
	}

	if len(outcome.Hits) != 4 {
		t.Fatalf("库里只有 4 篇，应如实返回 4 篇，实际 %d", len(outcome.Hits))
	}
	if len(searcher.budgets) != 1 {
		t.Fatalf("见底之后不该再加码，实际预算序列 %v", searcher.budgets)
	}
	// 条数偏少的原因必须能从台账里读出来：预算 80、只拿到 20 块。
	if outcome.ChunkBudget != 80 || outcome.MatchedChunks != 20 {
		t.Fatalf("台账应为预算 80 / 命中 20 块，实际 %d / %d", outcome.ChunkBudget, outcome.MatchedChunks)
	}
}

// chunk 粒度下预算与条数一一对应，不放大也不加码。
//
// 放大在这里是有害的：普通文档库里一篇长文切成几百块，多取几倍候选只会拉长延迟，
// 换不来任何「更完整的条数」—— 每一条本来就是一个分块。
func TestRecallKeepsChunkGranularityToOneRound(t *testing.T) {
	searcher := &poolSearcher{pool: productPool(20, 5)}
	service := newTestService(searcher)

	outcome, err := service.recall(context.Background(), "固装铰链", 9, grouping.Chunk, 5)
	if err != nil {
		t.Fatalf("取数失败: %v", err)
	}

	if len(outcome.Hits) != 5 {
		t.Fatalf("chunk 粒度下应返回 5 条，实际 %d", len(outcome.Hits))
	}
	if len(searcher.budgets) != 1 || searcher.budgets[0] != 5 {
		t.Fatalf("chunk 粒度下应按 5 块取一次，实际预算序列 %v", searcher.budgets)
	}
	if outcome.Granularity != grouping.Chunk {
		t.Fatalf("粒度台账不对: %q", outcome.Granularity)
	}
}

// 检索失败要原样上抛，不能被当成「没取满」而反复重试。
//
// 重试一个已经明确报错的检索，等于把一次故障放大成几倍延迟与几倍错误日志。
func TestRecallPropagatesSearchError(t *testing.T) {
	wantErr := errors.New("milvus: unavailable")
	service := newTestService(&poolSearcher{err: wantErr})

	if _, err := service.recall(context.Background(), "q", 9, grouping.Document, 20); !errors.Is(err, wantErr) {
		t.Fatalf("应原样返回检索错误，实际 %v", err)
	}
}

// stubContent 是正文端口的桩：记住被问了哪些文档，按 id 给出正文。
//
// 它替掉的是过去那个「往临时目录写一份文件再让 Service 去读」的桩 —— 那时
// 「补正文」这条路径的输入是文件系统，现在是一列数据，桩也就简单成一张表。
type stubContent struct {
	contents map[uint64]string
	err      error
	calls    int
	asked    []uint64
}

func (s *stubContent) Contents(_ context.Context, documentIDs []uint64) (map[uint64]string, error) {
	s.calls++
	s.asked = append(s.asked, documentIDs...)
	if s.err != nil {
		return nil, s.err
	}
	out := make(map[uint64]string, len(documentIDs))
	for _, id := range documentIDs {
		if content, ok := s.contents[id]; ok {
			out[id] = content
		}
	}
	return out, nil
}

// 按文档归并时，正文要换成**整篇文档**，不是代表块。
//
// 产品型录里一篇文档就是一个产品：型号、系列、规格表与描述散在好几个块里，只回
// 命中块等于把一个产品拆开只给一半 —— 下游拿不到规格，而响应里看不出任何异常，
// 只是内容少了一截。
func TestFillFullContentReplacesRepresentativeChunk(t *testing.T) {
	full := "# H105P 固装铰链\n\n型号：H105P\n开门角度：105°\n"
	contents := &stubContent{contents: map[uint64]string{7: full}}

	service := &Service{contents: contents, limits: testLimits(), logger: discardLogger()}
	hits := []SearchHit{{DocumentID: 7, Content: "代表块（只是文档里的一段）"}}

	filled := service.fillFullContent(context.Background(), hits, grouping.Document)

	if filled[0].Content != full {
		t.Fatalf("正文应补成整篇文档，实际 %q", filled[0].Content)
	}
	if filled[0].Truncated {
		t.Fatal("没超上限不该标记截断")
	}
}

// 一次召回只查一次库，且不重复问同一个文档。
//
// 逐条查会让往返次数跟着 top_k 涨（document 粒度下就是 20 次），而同一篇文档
// 命中好几块时（归并前）还会把同一个 id 问好几遍。两种都是「按条数线性放大
// 数据库压力」，而代价换不来任何东西。
func TestFillFullContentQueriesEachDocumentOnce(t *testing.T) {
	contents := &stubContent{contents: map[uint64]string{1: "第一篇", 2: "第二篇"}}
	service := &Service{contents: contents, limits: testLimits(), logger: discardLogger()}

	hits := []SearchHit{
		{DocumentID: 2, Content: "b1"},
		{DocumentID: 1, Content: "a1"},
		{DocumentID: 2, Content: "b2"},
	}
	service.fillFullContent(context.Background(), hits, grouping.Document)

	if contents.calls != 1 {
		t.Fatalf("应当只查一次库，实际 %d 次", contents.calls)
	}
	if len(contents.asked) != 2 {
		t.Fatalf("去重后应当只问 2 个文档，实际问了 %v", contents.asked)
	}
}

// chunk 粒度下不动正文：那一条结果**就是**一个分块。
//
// 换成全文等于把粒度悄悄改成文档，调用方按分块做的引用、去重与定位会全部错位。
// 连库都不该查：这条路径上多出来的那次查询是纯粹的浪费，而且它一旦存在，就有人
// 会顺手在这里改正文。
func TestFillFullContentLeavesChunkGranularityAlone(t *testing.T) {
	contents := &stubContent{contents: map[uint64]string{7: "# H105P 固装铰链\n\n型号：H105P\n"}}
	service := &Service{contents: contents, limits: testLimits(), logger: discardLogger()}
	hits := []SearchHit{{DocumentID: 7, Content: "这一块"}}

	filled := service.fillFullContent(context.Background(), hits, grouping.Chunk)

	if filled[0].Content != "这一块" {
		t.Fatalf("chunk 粒度下正文应原样保留，实际 %q", filled[0].Content)
	}
	if contents.calls != 0 {
		t.Fatalf("chunk 粒度下不该查库，实际查了 %d 次", contents.calls)
	}
}

// 正文取不回来时退回代表块正文，而不是给空内容。
//
// 读者要的是「尽量多的内容」，不是「要么全给要么不给」：代表块至少是文档里最贴近
// 查询的那一段。两种取不回来的形态都要覆盖 —— 查库失败（运维异常）与那条文档
// 确实没有正文内容（存量文档还没导入）。
func TestFillFullContentKeepsRepresentativeChunkWhenContentUnavailable(t *testing.T) {
	cases := []struct {
		name     string
		contents DocumentContent
		hit      SearchHit
	}{
		{
			name:     "查库失败",
			contents: &stubContent{err: errors.New("database is down")},
			hit:      SearchHit{DocumentID: 1, Content: "代表块"},
		},
		{
			name:     "文档没有正文",
			contents: &stubContent{contents: map[uint64]string{1: "   "}},
			hit:      SearchHit{DocumentID: 1, Content: "代表块"},
		},
		{
			name:     "命中没有 document_id",
			contents: &stubContent{},
			hit:      SearchHit{Content: "代表块"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service := &Service{contents: tc.contents, limits: testLimits(), logger: discardLogger()}

			filled := service.fillFullContent(context.Background(), []SearchHit{tc.hit}, grouping.Document)

			if filled[0].Content != "代表块" {
				t.Fatalf("取不到全文时应保留代表块正文，实际 %q", filled[0].Content)
			}
			if filled[0].Truncated {
				t.Fatal("没读到内容不该标记截断")
			}
		})
	}
}

// 没有正文端口（单测直接构造 Service、或换了部署形态）时同样退回代表块，不报错。
func TestFillFullContentWithoutContentReaderKeepsChunk(t *testing.T) {
	service := &Service{limits: testLimits(), logger: discardLogger()}
	hits := []SearchHit{{DocumentID: 1, Source: "documents/1/a.md", Content: "代表块"}}

	filled := service.fillFullContent(context.Background(), hits, grouping.Document)

	if filled[0].Content != "代表块" {
		t.Fatalf("没有正文端口时应保留代表块正文，实际 %q", filled[0].Content)
	}
}

// 超过上限的整篇文档要截断并**显式标记**。
//
// 悄悄截断是这里最坏的结果：调用方拿到的是一份看起来完整的产品规格，缺失的部分
// 没有任何痕迹，下游会照着半份规格做判断。
func TestFillFullContentTruncatesAndFlags(t *testing.T) {
	full := strings.Repeat("铰链规格。", 100) // 500 字节
	contents := &stubContent{contents: map[uint64]string{7: full}}

	limits := Limits{MaxContentBytes: 32}.withDefaults()
	service := &Service{contents: contents, limits: limits, logger: discardLogger()}

	filled := service.fillFullContent(context.Background(), []SearchHit{{DocumentID: 7}}, grouping.Document)

	if !filled[0].Truncated {
		t.Fatal("超上限应标记截断")
	}
	if len(filled[0].Content) != 30 {
		t.Fatalf("应按汉字边界回退到 30 字节，实际 %d 字节", len(filled[0].Content))
	}
	if !utf8.ValidString(filled[0].Content) {
		t.Fatalf("截断后必须是合法 UTF-8，实际 %q", filled[0].Content)
	}
	if !strings.HasPrefix(full, filled[0].Content) {
		t.Fatal("截断结果必须是原文的前缀")
	}
}

// 截断点回退到 rune 边界。
//
// 从一个汉字中间切开会得到无效 UTF-8，JSON 编码时被替换成 U+FFFD：调用方看到的
// 是一个坏字符，而看不出「这里被截断了」。
func TestTruncateContentKeepsRuneBoundary(t *testing.T) {
	content := "铰链铰链" // 4 字 / 12 字节

	got, truncated := truncateContent(content, 7)
	if !truncated {
		t.Fatal("超过上限应报告截断")
	}
	if got != "铰链" {
		t.Fatalf("7 字节应回退到 6 字节处，实际 %q（%d 字节）", got, len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果是非法 UTF-8: %q", got)
	}

	// 不超过上限：原样返回，且不标记。
	if got, truncated = truncateContent(content, 12); truncated || got != content {
		t.Fatalf("未超上限应原样返回且不标记，实际 truncated=%v content=%q", truncated, got)
	}
	// 上限 <=0 表示不截断（配置没给值时的缺省语义）。
	if got, truncated = truncateContent(content, 0); truncated || got != content {
		t.Fatalf("上限为 0 应不截断，实际 truncated=%v content=%q", truncated, got)
	}
}

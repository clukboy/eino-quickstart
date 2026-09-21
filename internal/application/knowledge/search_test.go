package knowledge

import (
	"context"
	"errors"
	"strings"
	"testing"

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
// 类型的数据集跑同一批命中，断言结果条数确实不同。
func TestApplyGranularityResolvesFromDatasetType(t *testing.T) {
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

	product, productGranularity := service.applyGranularity(&ent.Dataset{Type: "product"}, hits)
	if productGranularity != grouping.Document {
		t.Fatalf("product 库应归并到文档，实际 %q", productGranularity)
	}
	if len(product) != 1 {
		t.Fatalf("product 库两块的同一篇文档应归并成 1 条，实际 %d", len(product))
	}

	plain, plainGranularity := service.applyGranularity(&ent.Dataset{Type: "text"}, hits)
	if plainGranularity != grouping.Chunk {
		t.Fatalf("text 库应保持分块粒度，实际 %q", plainGranularity)
	}
	if len(plain) != 2 {
		t.Fatalf("text 库按分块返回，期望 2 条，实际 %d", len(plain))
	}
}

// 数据集没有 type（默认空串）时走兜底粒度，不能报错也不能丢结果。
func TestApplyGranularityFallsBackWhenTypeIsEmpty(t *testing.T) {
	service := &Service{}

	got, granularity := service.applyGranularity(
		&ent.Dataset{},
		[]SearchHit{{Source: "documents/2/a.md", Score: 1}},
	)

	if granularity != grouping.Default {
		t.Fatalf("空类型应取默认粒度 %q，实际 %q", grouping.Default, granularity)
	}
	if len(got) != 1 {
		t.Fatalf("命中的 1 条不该丢，实际 %d", len(got))
	}
}

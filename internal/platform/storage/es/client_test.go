package es

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"eino-quickstart/internal/platform/config"
)

// 这个文件不用真集群：要验证的是「我们发出去的 DSL 长什么样」，而不是 ES 会
// 怎么执行它 —— 后者由 ES 自己保证。把断言放在请求体上，mapping 与查询契约
// 才能在没有集群的环境（CI、本地）里被守住，否则这些约定只有到线上才能发现
// 写错了，而错了的表现是「检索质量变差」，不是报错。

// shippedMappingPath 指向真正发给运维的那份映射文件。
//
// 测试读真文件而不是 testdata 里的一份副本：副本一定会漂移，而漂移的方式
// 恰好是测试全绿、生产行为已经变了。路径是相对包目录的仓库根。
const shippedMappingPath = "../../../../configs/es/chunk_mapping.yaml"

// fakeES 是只记录请求、返回固定响应的假集群。
type fakeES struct {
	*httptest.Server

	mu      sync.Mutex
	paths   []string
	bodies  []string
	respond func(method, path string) (int, string)
}

func newFakeES(t *testing.T, respond func(method, path string) (int, string)) *fakeES {
	t.Helper()
	fake := &fakeES{respond: respond}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		fake.mu.Lock()
		fake.paths = append(fake.paths, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		fake.bodies = append(fake.bodies, string(body))
		fake.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		// Go 客户端会校验这个头，缺了会报「not Elasticsearch」而不是给出响应，
		// 失败信息与真实原因完全无关。
		w.Header().Set("X-Elastic-Product", "Elasticsearch")

		status, payload := fake.respond(r.Method, r.URL.Path)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, payload)
	}))
	t.Cleanup(fake.Server.Close)
	return fake
}

func (f *fakeES) lastBody(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		t.Fatal("假集群没有收到任何请求")
	}
	return f.bodies[len(f.bodies)-1]
}

func (f *fakeES) lastPath(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.paths) == 0 {
		t.Fatal("假集群没有收到任何请求")
	}
	return f.paths[len(f.paths)-1]
}

// newTestClient 构造一个指向假集群的客户端。
func newTestClient(t *testing.T, address string, mutate ...func(*config.ESConfig)) *Client {
	t.Helper()
	cfg := &config.ESConfig{
		Address:     []string{address},
		Index:       "chunks_v1",
		Analyzer:    "ik_max_word",
		BulkSize:    2,
		MappingFile: shippedMappingPath,
	}
	for _, apply := range mutate {
		apply(cfg)
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("构造客户端: %v", err)
	}
	if client == nil {
		t.Fatal("配了地址却拿到 nil 客户端")
	}
	return client
}

// okSearchResponse 造一个「分数按传入顺序递减」的响应，用来验证名次被原样保留。
func okSearchResponse(ids ...string) string {
	items := make([]string, 0, len(ids))
	for k, id := range ids {
		score := float64(len(ids)-k) + 0.5
		items = append(items, fmt.Sprintf(
			`{"_index":"chunks_v1","_id":%q,"_score":%v}`, id, score))
	}
	return fmt.Sprintf(
		`{"took":1,"hits":{"total":{"value":%d},"hits":[%s]}}`,
		len(ids), strings.Join(items, ","))
}

// TestNewDisabledWithoutAddress 钉住「没有 ES 是合法状态」这条约定：
// 返回 nil 而不是错误，调用方才敢用一句判空走 PostgreSQL 回落。
func TestNewDisabledWithoutAddress(t *testing.T) {
	client, err := New(&config.ESConfig{})
	if err != nil {
		t.Fatalf("地址为空不该报错: %v", err)
	}
	if client != nil {
		t.Fatal("地址为空应当返回 nil 客户端")
	}

	client, err = New(nil)
	if err != nil || client != nil {
		t.Fatalf("nil 配置应当返回 (nil, nil)，实际 (%v, %v)", client, err)
	}
}

// TestNewRequiresMappingFile 钉住「检索面必须显式声明」这条约定。
//
// 少了它，进程能起来、能连集群、也能写入 —— 只是写进去的文档没有映射声明的
// 字段，按型号搜永远搜不到。让它在启动时就失败，比让它静默降级好。
func TestNewRequiresMappingFile(t *testing.T) {
	_, err := New(&config.ESConfig{
		Address:  []string{"http://127.0.0.1:9200"},
		Index:    "chunks_v1",
		Analyzer: "ik_max_word",
	})
	if err == nil {
		t.Fatal("配了地址但没给映射文件，应当报错")
	}
	if !strings.Contains(err.Error(), "mappingFile") {
		t.Errorf("错误信息应当提到 mappingFile，实际: %v", err)
	}

	_, err = New(&config.ESConfig{
		Address:     []string{"http://127.0.0.1:9200"},
		Index:       "chunks_v1",
		Analyzer:    "ik_max_word",
		MappingFile: "testdata/不存在的映射.yaml",
	})
	if err == nil {
		t.Fatal("映射文件不存在应当报错")
	}
}

// TestBasePropertiesCarryAnalyzer 守住共有字段的两条命门：
// 每个可打分的 text 字段都带上了配置的分词器（漏了就退化成 standard，中文被
// 切成单字），以及 source 有原样的 keyword 子字段（否则回指原文的键会被分词器
// 改写，命中之后按它去 documents.source 就查不到行）。
func TestBasePropertiesCarryAnalyzer(t *testing.T) {
	client := newTestClient(t, "http://127.0.0.1:9200")
	properties, err := client.mapping.properties("ik_max_word")
	if err != nil {
		t.Fatalf("造属性表: %v", err)
	}
	decoded := decodeProperties(t, properties)

	for _, field := range []string{fieldContent, fieldTitle, fieldHeadingPath, fieldSource, fieldMetadataText} {
		property, ok := decoded[field]
		if !ok {
			t.Fatalf("属性表缺少字段 %q", field)
		}
		if property.Type != "text" {
			t.Errorf("%s 的类型是 %q，期望 text", field, property.Type)
		}
		if property.Analyzer != "ik_max_word" {
			t.Errorf("%s 的分词器是 %q，期望 ik_max_word（漏了就会退化成 standard）", field, property.Analyzer)
		}
	}

	for _, field := range []string{fieldSource, fieldTitle, fieldHeadingPath} {
		if _, ok := decoded[field].Fields["keyword"]; !ok {
			t.Errorf("%s 缺少 keyword 子字段，无法原样取回", field)
		}
	}
	if _, ok := decoded[fieldContent].Fields["keyword"]; ok {
		t.Error("content 不该有 keyword 子字段：正文超长，keyword 子字段写不进去还白占空间")
	}
	if _, ok := decoded[fieldMetadataText].Fields["keyword"]; ok {
		t.Error("兜底字段不该有 keyword 子字段：值可能很长，且没人拿它做精确过滤")
	}

	for field, want := range map[string]string{
		fieldChunkID:    "keyword",
		fieldDocumentID: "keyword",
		fieldDatasetID:  "keyword",
		fieldChunkIndex: "integer",
		fieldVisibility: "keyword",
		fieldOwner:      "keyword",
		fieldIndexedAt:  "date",
		fieldMappingRev: "keyword",
	} {
		if got := decoded[field].Type; got != want {
			t.Errorf("%s 的类型是 %q，期望 %q", field, got, want)
		}
	}
}

// TestMappingDeclaresMetadataSurface 守住「按型号搜得到」这件事的物理前提：
// 元数据字段必须真的在属性表里，而且是带分词器的 text。
//
// 不成立时的表现是静默的：查询里的 model^5 变成一个永远命中不了的条款，
// ES 不会报错，只会让那一类查询排到后面。
func TestMappingDeclaresMetadataSurface(t *testing.T) {
	client := newTestClient(t, "http://127.0.0.1:9200")
	properties, err := client.mapping.properties("cjk")
	if err != nil {
		t.Fatalf("造属性表: %v", err)
	}
	decoded := decodeProperties(t, properties)

	for _, field := range []string{
		"product_id", "model", "product_name", "series_name",
		"family_prefix", "category_l1", "category_l2", "variants",
	} {
		property, ok := decoded[field]
		if !ok {
			t.Fatalf("映射声明的字段 %q 不在属性表里", field)
		}
		if property.Type != "text" || property.Analyzer != "cjk" {
			t.Errorf("%s 是 %q/%q，期望 text/cjk", field, property.Type, property.Analyzer)
		}
		if _, ok := property.Fields["keyword"]; !ok {
			t.Errorf("%s 缺少 keyword 子字段，无法按型号精确过滤", field)
		}
	}
}

// TestSearchFieldsRankMetadataAboveContent 钉住权重关系：同一个词出现在元数据里
// 比出现在正文里更能说明「就是这篇」。
//
// 断言的是相对关系而不是具体数字 —— 数字会随调优变，关系不该变：一改就说明
// 有人在调整检索面的优先级，那时应该是有意的。
func TestSearchFieldsRankMetadataAboveContent(t *testing.T) {
	fields := newTestClient(t, "http://127.0.0.1:9200").mapping.SearchFields()
	weight := func(field string) int {
		for _, entry := range fields {
			name, boost, found := strings.Cut(entry, "^")
			if name != field {
				continue
			}
			if !found {
				return 1
			}
			value, err := strconv.Atoi(boost)
			if err != nil {
				t.Fatalf("%s 的权重 %q 不是整数", field, boost)
			}
			return value
		}
		t.Fatalf("打分字段表里没有 %s（实际 %v）", field, fields)
		return 0
	}

	content := weight(fieldContent)
	for _, field := range []string{
		"model", "product_id", "product_name", "series_name",
		"family_prefix", "category_l1", "category_l2", "variants", fieldMetadataText,
	} {
		if weight(field) <= content {
			t.Errorf("%s 的权重不高于 content（%d），元数据命中会被正文淹没", field, content)
		}
	}
	// 型号是业务主键式的信息，权重最高。
	for _, field := range []string{fieldTitle, fieldSource, "product_name", "series_name"} {
		if weight("model") <= weight(field) {
			t.Errorf("%s 的权重不低于 model，型号命中不再是最强信号", field)
		}
	}
}

// TestSearchChunksSendsBM25MultiMatch 是这次接入的核心断言：查询串原样透传给
// ES，不在本地切词；打分字段与权重和映射派生出来的一致；关掉 _source。
func TestSearchChunksSendsBM25MultiMatch(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		return http.StatusOK, okSearchResponse("42", "7")
	})
	client := newTestClient(t, fake.URL)

	hits, err := client.SearchChunks(context.Background(), "H105G 铰链", 5)
	if err != nil {
		t.Fatalf("检索: %v", err)
	}

	if got := fake.lastPath(t); !strings.Contains(got, "/chunks_v1/_search") {
		t.Errorf("请求路径是 %q，期望打到 chunks_v1 的 _search", got)
	}

	var request struct {
		Size   *int `json:"size"`
		Source any  `json:"_source"`
		Query  struct {
			MultiMatch struct {
				Query  string   `json:"query"`
				Fields []string `json:"fields"`
				Type   string   `json:"type"`
			} `json:"multi_match"`
		} `json:"query"`
	}
	if err := json.Unmarshal([]byte(fake.lastBody(t)), &request); err != nil {
		t.Fatalf("解析查询体: %v", err)
	}

	if request.Size == nil || *request.Size != 5 {
		t.Errorf("size 是 %v，期望 5", request.Size)
	}
	if request.Source != false {
		t.Errorf("_source 是 %v，期望 false（命中只要 _id，正文回 PostgreSQL 取）", request.Source)
	}
	if request.Query.MultiMatch.Query != "H105G 铰链" {
		t.Errorf("查询串是 %q，期望原样透传", request.Query.MultiMatch.Query)
	}
	if request.Query.MultiMatch.Type != "best_fields" {
		t.Errorf("multi_match 类型是 %q，期望 best_fields", request.Query.MultiMatch.Type)
	}
	// 字段表必须由映射派生，而不是本地常量：否则改映射时查询侧不会跟着变。
	want := client.mapping.SearchFields()
	if diff := diffStrings(request.Query.MultiMatch.Fields, want); diff != "" {
		t.Errorf("打分字段与映射派生结果不一致: %s", diff)
	}
	for _, field := range []string{"model^5", "content"} {
		if !containsString(want, field) {
			t.Errorf("字段表里缺少 %q（实际 %v）", field, want)
		}
	}

	// 名次必须原样保留：调用方（融合）只用名次不用分数绝对值。
	if len(hits) != 2 || hits[0].ChunkID != 42 || hits[1].ChunkID != 7 {
		t.Fatalf("命中解析错误: %+v", hits)
	}
	if hits[0].Score <= hits[1].Score {
		t.Errorf("分数顺序被改动: %+v", hits)
	}
}

// TestSearchExactUsesTermOnKeywordSubfields 守住精确通道的查询形状。
//
// 它必须是 term（逐字相等）而不是 match（分词）：打在公司名、型号这类值上时，
// 分词匹配会把「图特」也命中「图特股份」这一类包含关系，那就不是「精确」了。
func TestSearchExactUsesTermOnKeywordSubfields(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		return http.StatusOK, `{"took":1,"hits":{"hits":[{"_id":"11","_score":4.2},{"_id":"12","_score":1.1}]}}`
	})
	client := newTestClient(t, fake.URL)

	hits, err := client.SearchExact(context.Background(), "H105P 铰链", 5)
	if err != nil {
		t.Fatalf("精确检索: %v", err)
	}

	var request struct {
		Size   *int `json:"size"`
		Source any  `json:"_source"`
		Query  struct {
			Bool struct {
				Should []struct {
					Term map[string]struct {
						Value string  `json:"value"`
						Boost float64 `json:"boost"`
					} `json:"term"`
				} `json:"should"`
			} `json:"bool"`
		} `json:"query"`
	}
	if err := json.Unmarshal([]byte(fake.lastBody(t)), &request); err != nil {
		t.Fatalf("解析查询体: %v", err)
	}
	if request.Source != false {
		t.Errorf("_source 是 %v，期望 false", request.Source)
	}

	// 切成两个词之后，每个词都要对每个精确字段有一条 term 子句 ——
	// 「H105P 铰链」这种型号混在句子里的输入，整串去比是比不中的。
	exact := client.mapping.ExactFields()
	if len(exact) == 0 {
		t.Fatal("随仓库分发的映射应当声明了 keyword 子字段")
	}
	seen := make(map[string]map[string]float64) // 词 -> 字段 -> 权重
	for _, clause := range request.Query.Bool.Should {
		for name, term := range clause.Term {
			if strings.HasSuffix(name, ".keyword") == false {
				t.Errorf("term 打在了 %s 上，精确匹配必须打 .keyword 子字段", name)
			}
			if seen[term.Value] == nil {
				seen[term.Value] = make(map[string]float64)
			}
			seen[term.Value][name] = term.Boost
		}
	}
	for _, word := range []string{"H105P", "铰链"} {
		if len(seen[word]) != len(exact) {
			t.Errorf("词 %q 的 term 子句有 %d 条，期望每个精确字段一条（共 %d）",
				word, len(seen[word]), len(exact))
		}
	}
	for _, field := range exact {
		if got, ok := seen["H105P"][field.Name]; !ok || got != field.Boost {
			t.Errorf("字段 %s 的权重是 %v，期望 %v", field.Name, got, field.Boost)
		}
	}

	if len(hits) != 2 || hits[0].ChunkID != 11 {
		t.Fatalf("命中解析错误: %+v", hits)
	}
}

// TestSearchExactWithoutKeywordFieldsSkipsRequest 守住「映射没声明 keyword 时
// 这条通道安静地不参与」。没有可打的字段还发请求，只会换来一次必然为空的往返。
func TestSearchExactWithoutKeywordFieldsSkipsRequest(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		t.Error("没有精确字段时不该发出请求")
		return http.StatusOK, "{}"
	})
	client := newTestClient(t, fake.URL)
	client.mapping = &Mapping{Fields: []FieldSpec{
		{Name: "model", From: "model", Type: FieldText, Boost: 5},
	}}

	hits, err := client.SearchExact(context.Background(), "H105P", 5)
	if err != nil {
		t.Fatalf("精确检索: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("不该有命中，实际 %+v", hits)
	}
}

func TestSearchChunksRejectsEmptyQuery(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		t.Error("空查询不该发出请求：ES 会把它当成 match_all")
		return http.StatusOK, "{}"
	})
	client := newTestClient(t, fake.URL)

	if _, err := client.SearchChunks(context.Background(), "   ", 5); err == nil {
		t.Fatal("空查询应当报错")
	}
	if _, err := client.SearchChunks(context.Background(), "x", 0); err == nil {
		t.Fatal("topK=0 应当报错")
	}
}

// TestSearchChunksSkipsNonChunkIDs 覆盖索引里混进外来文档的情况：
// 跳过，而不是让整次检索失败。
func TestSearchChunksSkipsNonChunkIDs(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		return http.StatusOK, `{"took":1,"hits":{"hits":[
			{"_index":"chunks_v1","_id":"abc-不是分块","_score":9.0},
			{"_index":"chunks_v1","_id":"11","_score":3.0}]}}`
	})
	client := newTestClient(t, fake.URL)

	hits, err := client.SearchChunks(context.Background(), "铰链", 5)
	if err != nil {
		t.Fatalf("检索: %v", err)
	}
	if len(hits) != 1 || hits[0].ChunkID != 11 {
		t.Fatalf("脏文档没有被跳过: %+v", hits)
	}
}

// TestIndexChunksWritesIDAndBatches 钉住幂等键与分批提交：
// _id 必须是 chunk_id（重试覆盖而不是追加），且按 bulkSize 拆批。
func TestIndexChunksWritesIDAndBatches(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		return http.StatusOK, `{"took":1,"errors":false,"items":[]}`
	})
	client := newTestClient(t, fake.URL) // bulkSize=2

	docs := []ChunkDoc{
		{ChunkID: "1", DocumentID: "9", Content: "第一段"},
		{ChunkID: "2", DocumentID: "9", Content: "第二段"},
		{ChunkID: "3", DocumentID: "9", Content: "第三段"},
	}
	if err := client.IndexChunks(context.Background(), docs); err != nil {
		t.Fatalf("写入: %v", err)
	}

	fake.mu.Lock()
	requests := append([]string{}, fake.bodies...)
	fake.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("bulkSize=2 写 3 条应当分成 2 个请求，实际 %d 个", len(requests))
	}

	lines := strings.Split(strings.TrimSpace(requests[0]), "\n")
	if len(lines) != 4 {
		t.Fatalf("第一个请求应当是 2 组「动作行+文档行」，实际 %d 行: %s", len(lines), requests[0])
	}
	if lines[0] != `{"index":{"_id":"1"}}` {
		t.Errorf("动作行是 %q，期望带上 _id=chunk_id", lines[0])
	}
	if !strings.Contains(lines[1], `"chunk_id":"1"`) {
		t.Errorf("文档行缺少 chunk_id: %s", lines[1])
	}
	if lines[2] != `{"index":{"_id":"2"}}` {
		t.Errorf("第二个动作行是 %q", lines[2])
	}
}

// TestIndexChunksSurfacesPartialFailures 覆盖 bulk 的「HTTP 200 但部分失败」：
// 这种情况必须报错，否则表现为「写入成功，但某些分块就是搜不到」。
func TestIndexChunksSurfacesPartialFailures(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		return http.StatusOK, `{"took":1,"errors":true,"items":[
			{"index":{"_index":"chunks_v1","_id":"1","status":200}},
			{"index":{"_index":"chunks_v1","_id":"2","status":400,
			 "error":{"type":"mapper_parsing_exception","reason":"failed to parse field [chunk_index]"}}}]}`
	})
	client := newTestClient(t, fake.URL)

	err := client.IndexChunks(context.Background(), []ChunkDoc{
		{ChunkID: "1"}, {ChunkID: "2"},
	})
	if err == nil {
		t.Fatal("部分写入失败必须报错")
	}
	if !strings.Contains(err.Error(), "chunk 2") || !strings.Contains(err.Error(), "failed to parse field") {
		t.Errorf("错误信息应当带上失败条目与原因，实际: %v", err)
	}
}

func TestIndexChunksRejectsMissingID(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		t.Error("缺少 _id 时不该发出请求")
		return http.StatusOK, "{}"
	})
	client := newTestClient(t, fake.URL)

	if err := client.IndexChunks(context.Background(), []ChunkDoc{{ChunkID: ""}}); err == nil {
		t.Fatal("缺少 chunk_id 应当报错：没有幂等键就会写出永远清不掉的孤儿")
	}
}

// TestDeleteByDocumentRequestsRefresh 钉住两个参数：conflicts=proceed 让并发
// 写入不至于让清理失败，refresh=true 让随后的查询立刻看不到旧分块。
func TestDeleteByDocumentRequestsRefresh(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		return http.StatusOK, `{"took":1,"deleted":3}`
	})
	client := newTestClient(t, fake.URL)

	deleted, err := client.DeleteByDocument(context.Background(), 9)
	if err != nil {
		t.Fatalf("清理: %v", err)
	}
	if deleted != 3 {
		t.Errorf("删除条数是 %d，期望 3", deleted)
	}

	path := fake.lastPath(t)
	if !strings.Contains(path, "/chunks_v1/_delete_by_query") {
		t.Errorf("请求路径是 %q", path)
	}
	if !strings.Contains(path, "conflicts=proceed") {
		t.Errorf("请求缺少 conflicts=proceed: %q", path)
	}
	if !strings.Contains(path, "refresh=true") {
		t.Errorf("请求缺少 refresh=true: %q", path)
	}
	if body := fake.lastBody(t); !strings.Contains(body, `"document_id":{"value":"9"}`) {
		t.Errorf("删除条件不是按 document_id: %s", body)
	}
}

// TestEnsureIndexDetectsAnalyzerDrift 守住最容易踩的坑：改了 es.analyzer 却忘了
// 换 es.index。不报出来的话，症状是检索命中率悄悄变差，排查方向会跑到 embedding
// 和切块策略上去。
func TestEnsureIndexDetectsAnalyzerDrift(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		switch {
		case method == http.MethodHead:
			return http.StatusOK, "" // 索引已存在
		case strings.HasSuffix(path, "/_mapping"):
			return http.StatusOK, `{"chunks_v1":{"mappings":{"properties":{
				"content":{"type":"text","analyzer":"cjk"}}}}}`
		default:
			t.Errorf("意外的请求 %s %s", method, path)
			return http.StatusOK, "{}"
		}
	})
	client := newTestClient(t, fake.URL) // 配置的是 ik_max_word

	err := client.EnsureIndex(context.Background())
	if err == nil {
		t.Fatal("索引分词器与配置不一致必须报错")
	}
	for _, want := range []string{"cjk", "ik_max_word", "_v2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息应当提到 %q，实际: %v", want, err)
		}
	}
}

func TestEnsureIndexAcceptsMatchingAnalyzer(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		switch {
		case method == http.MethodHead:
			return http.StatusOK, ""
		case strings.HasSuffix(path, "/_mapping"):
			return http.StatusOK, `{"chunks_v1":{"mappings":{"properties":{
				"content":{"type":"text","analyzer":"ik_max_word"}}}}}`
		default:
			return http.StatusOK, "{}"
		}
	})
	if err := newTestClient(t, fake.URL).EnsureIndex(context.Background()); err != nil {
		t.Fatalf("分词器一致不该报错: %v", err)
	}
}

// TestEnsureIndexBackfillsMapping 守住索引演进的路径。
//
// 往已有索引里加字段是 ES 允许的原地操作，所以「给元数据加检索面」这类改动
// 不该逼运维换索引名重建。但如果代码里不主动补写，新字段就永远不存在 ——
// 查询里的 model^5 变成一个永远命中不了的条款，ES 一声不响。
func TestEnsureIndexBackfillsMapping(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		switch {
		case method == http.MethodHead:
			return http.StatusOK, ""
		case method == http.MethodGet && strings.HasSuffix(path, "/_mapping"):
			return http.StatusOK, `{"chunks_v1":{"mappings":{"properties":{
				"content":{"type":"text","analyzer":"ik_max_word"}}}}}`
		case method == http.MethodPut && strings.HasSuffix(path, "/_mapping"):
			return http.StatusOK, `{"acknowledged":true}`
		default:
			t.Errorf("意外的请求 %s %s", method, path)
			return http.StatusOK, "{}"
		}
	})
	if err := newTestClient(t, fake.URL).EnsureIndex(context.Background()); err != nil {
		t.Fatalf("补写已有索引的 mapping: %v", err)
	}

	body := fake.lastBody(t)
	for _, want := range []string{
		`"model"`, `"metadata_text"`, `"mapping_rev"`, `"analyzer":"ik_max_word"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("补写的 mapping 里缺少 %s: %s", want, body)
		}
	}
	// 动态映射必须一并纠正：老索引可能是 dynamic=true 时建的，不纠正的话
	// 映射文件里漏声明的字段会被 ES 悄悄加成 standard 分词的 text。
	if !strings.Contains(body, `"dynamic":"false"`) {
		t.Errorf("补写的 mapping 里缺少 dynamic=false: %s", body)
	}
}

// TestEnsureIndexHintsWhenFieldTypeConflicts 覆盖唯一需要换索引名的那条路：
// 字段类型变了，PutMapping 会被拒绝，而 ES 的错误信息不说该怎么办。
func TestEnsureIndexHintsWhenFieldTypeConflicts(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		switch {
		case method == http.MethodHead:
			return http.StatusOK, ""
		case method == http.MethodGet:
			return http.StatusOK, `{"chunks_v1":{"mappings":{"properties":{
				"content":{"type":"text","analyzer":"ik_max_word"}}}}}`
		case method == http.MethodPut:
			return http.StatusBadRequest, `{"error":{"type":"illegal_argument_exception",
				"reason":"mapper [model] cannot be changed from type [keyword] to [text]"}}`
		default:
			return http.StatusOK, "{}"
		}
	})
	err := newTestClient(t, fake.URL).EnsureIndex(context.Background())
	if err == nil {
		t.Fatal("字段类型冲突必须报错")
	}
	for _, want := range []string{"cannot be changed", "_v2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息应当提到 %q，实际: %v", want, err)
		}
	}
}

// TestStaleDocsCountsDocumentsWithoutCurrentRev 钉住「陈旧文档」的判定口径：
// 形态指纹与当前映射不一致的（含完全没有指纹的老文档）都算，因为它们的检索面
// 是空的 —— 按型号搜不到它们，但那不是 BM25 失效。
func TestStaleDocsCountsDocumentsWithoutCurrentRev(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		return http.StatusOK, `{"count":7}`
	})
	client := newTestClient(t, fake.URL)

	stale, err := client.StaleDocs(context.Background())
	if err != nil {
		t.Fatalf("统计陈旧文档: %v", err)
	}
	if stale != 7 {
		t.Errorf("陈旧文档数是 %d，期望 7", stale)
	}

	if path := fake.lastPath(t); !strings.Contains(path, "/chunks_v1/_count") {
		t.Errorf("请求路径是 %q，期望打到 _count", path)
	}
	body := fake.lastBody(t)
	for _, want := range []string{
		`"must_not"`, `"term"`, `"mapping_rev"`, client.rev,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("统计条件里缺少 %s: %s", want, body)
		}
	}
}

func TestEnsureIndexCreatesWithAnalyzer(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		switch {
		case method == http.MethodHead:
			return http.StatusNotFound, ""
		case method == http.MethodPut:
			return http.StatusOK, `{"acknowledged":true}`
		default:
			return http.StatusOK, "{}"
		}
	})
	if err := newTestClient(t, fake.URL).EnsureIndex(context.Background()); err != nil {
		t.Fatalf("建索引: %v", err)
	}
	body := fake.lastBody(t)
	for _, want := range []string{`"analyzer":"ik_max_word"`, `"dynamic":"false"`} {
		if !strings.Contains(body, want) {
			t.Errorf("建索引请求里缺少 %s: %s", want, body)
		}
	}
}

func TestEnsureIndexHintsWhenAnalyzerUnknown(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		if method == http.MethodHead {
			return http.StatusNotFound, ""
		}
		return http.StatusBadRequest, `{"error":{"type":"illegal_argument_exception",
			"reason":"analyzer [ik_max_word] has not been configured in mappings"}}`
	})
	err := newTestClient(t, fake.URL).EnsureIndex(context.Background())
	if err == nil {
		t.Fatal("建索引失败必须报错")
	}
	if !strings.Contains(err.Error(), "cjk") {
		t.Errorf("分词器不存在时应当提示 cjk 这条退路，实际: %v", err)
	}
}

// TestChunkDocJSONShape 把写入契约钉在 JSON 上。
//
// 共有字段名一旦改动，检索侧的 multi_match 与属性表会静默失配；映射声明的业务
// 字段则必须真的摊平到文档顶层 —— 写成嵌套对象的话，ES 里会出现一个没人查询的
// 子字段，而查询里的 model^5 永远命中不了。
func TestChunkDocJSONShape(t *testing.T) {
	client := newTestClient(t, "http://127.0.0.1:9200")
	doc := ChunkDoc{
		ChunkID:     "12",
		DocumentID:  "3",
		DatasetID:   "8",
		ChunkIndex:  1,
		Source:      "H11_二段力滑入式铰链.md",
		Title:       "H11 铰链",
		HeadingPath: "规格",
		Content:     "H105G 的安装孔距",
		Visibility:  "private",
		Owner:       "developer-1",
		IndexedAt:   time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC),
		Metadata: map[string]any{
			"product_id":    "H105P",
			"model":         "H105P",
			"product_name":  "二段力小角度偏心轮快装缓冲铰链",
			"series_name":   "图冠系列",
			"family_prefix": "H105",
			"category_l1":   "铰链",
			"category_l2":   "缓冲铰链",
			"variants":      []any{"H105P-白", "H105P-黑"},
			"specs_from_doc": map[string]any{
				"surface_finish": "镀钛镍",
				"open_angle_deg": float64(100),
			},
			"source_doc": "图特知识库模版铰链-新(1)(1).docx",
		},
	}
	doc.enrich(client.mapping, client.rev)

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		`"chunk_id":"12"`, `"document_id":"3"`, `"dataset_id":"8"`, `"chunk_index":1`,
		`"source":"H11_二段力滑入式铰链.md"`, `"title":"H11 铰链"`, `"heading_path":"规格"`,
		`"content":"H105G 的安装孔距"`, `"visibility":"private"`, `"owner":"developer-1"`,
		`"product_id":"H105P"`, `"model":"H105P"`, `"product_name":"二段力小角度偏心轮快装缓冲铰链"`,
		`"series_name":"图冠系列"`, `"family_prefix":"H105"`, `"category_l1":"铰链"`,
		`"category_l2":"缓冲铰链"`, `"variants":["H105P-白","H105P-黑"]`,
		`"mapping_rev":"` + client.rev + `"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("写入的 JSON 缺少 %s: %s", want, body)
		}
	}
	// 没有单独成列的规格必须进兜底字段，否则「按门板材质搜」这一类会全灭。
	for _, want := range []string{"镀钛镍", "100", "图特知识库模版铰链-新(1)(1).docx"} {
		if !strings.Contains(body, want) {
			t.Errorf("兜底字段里缺少 %s: %s", want, body)
		}
	}
	// 元数据本身不该整块写进文档：它的形态是内部约定，写进去只会白占索引体积。
	if strings.Contains(body, `"Metadata"`) || strings.Contains(body, `"specs_from_doc":{`) {
		t.Errorf("元数据被整块写进了文档: %s", body)
	}
}

// TestChunkDocOmitsEmptyValues 钉住「没有值就不写字段」这条约定。
//
// 空串分词后没有词元、不影响打分，但每份文档都多几个字段，乘上百万分块就是
// 可观的索引体积；更要紧的是，它让「这个字段到底有没有值」变得看不出来 ——
// 而「字段存在但永远为空」正是映射路径写错时的表现。
func TestChunkDocOmitsEmptyValues(t *testing.T) {
	client := newTestClient(t, "http://127.0.0.1:9200")
	doc := ChunkDoc{
		ChunkID: "1",
		Content: "正文",
		Metadata: map[string]any{
			"model":    "",
			"variants": []any{},
			"note":     "   ",
		},
	}
	doc.enrich(client.mapping, client.rev)

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("序列化: %v", err)
	}
	body := string(raw)
	for _, absent := range []string{
		"product_id", "model", "product_name", "series_name",
		"family_prefix", "category_l1", "category_l2", "variants", "metadata_text",
	} {
		if strings.Contains(body, absent) {
			t.Errorf("空值不该写进文档，却出现了 %s: %s", absent, body)
		}
	}
	if !strings.Contains(body, `"mapping_rev":"`+client.rev+`"`) {
		t.Errorf("形态指纹必须总是写入: %s", body)
	}
}

// TestIndexChunksStampsMappingRev 钉住指纹由写入端盖章。
//
// 交给调用方去填的话，漏填一处就会让那批文档被预检当成「需要 reindex 的旧
// 文档」—— 一个只有运维才看得见的假警报，而且极难联想到根因。
func TestIndexChunksStampsMappingRev(t *testing.T) {
	fake := newFakeES(t, func(method, path string) (int, string) {
		return http.StatusOK, `{"took":1,"errors":false,"items":[]}`
	})
	client := newTestClient(t, fake.URL)

	// 调用方完全不管指纹，写入端必须补上当前值。
	if err := client.IndexChunks(context.Background(), []ChunkDoc{{ChunkID: "1"}}); err != nil {
		t.Fatalf("写入: %v", err)
	}
	if body := fake.lastBody(t); !strings.Contains(body, `"mapping_rev":"`+client.rev+`"`) {
		t.Errorf("写入端没有给文档盖形态指纹: %s", body)
	}
}

// decodedProperty 是断言用的属性视图。
//
// 不直接断言生成代码里的具体类型：ES 的属性在库里是联合类型（types.Property
// 就是 any），按 JSON 形态断言更贴近真实契约，也不会因为库升级换了结构体名
// 就让测试挂掉。
type decodedProperty struct {
	Type     string `json:"type"`
	Analyzer string `json:"analyzer"`
	Fields   map[string]struct {
		Type string `json:"type"`
	} `json:"fields"`
}

// decodeProperties 把属性表序列化后再解回来。
func decodeProperties(t *testing.T, properties any) map[string]decodedProperty {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"properties": properties})
	if err != nil {
		t.Fatalf("序列化属性表: %v", err)
	}
	var decoded struct {
		Properties map[string]decodedProperty `json:"properties"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("解析属性表: %v", err)
	}
	return decoded.Properties
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func diffStrings(got, want []string) string {
	if len(got) != len(want) {
		return "长度不同: " + strings.Join(got, ",") + " != " + strings.Join(want, ",")
	}
	for k := range got {
		if got[k] != want[k] {
			return "第 " + strconv.Itoa(k) + " 项不同: " + got[k] + " != " + want[k]
		}
	}
	return ""
}

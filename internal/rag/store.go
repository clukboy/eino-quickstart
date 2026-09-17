package rag

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag/constant"
	"eino-quickstart/internal/rag/store/milvus"
	"eino-quickstart/internal/rag/store/postgres"
	"eino-quickstart/pkg/convert"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/cloudwego/eino/schema"
)

// metaKeyDatasetID 是分块元数据里的数据集归属键。
//
// 它与 constant 包里那批键不同：那些是 chunk 在 pipeline 阶段流转用的，
// 这个是入库时由应用层补写的业务归属，检索侧只有影子定义（应用层是字面量）。
const metaKeyDatasetID = "dataset_id"

// candidateMultiplier 是单通道候选的放大倍数。
//
// 两个通道各自多取一些，给 ACL 过滤和 RRF 融合留余量：过滤是在检索之后做的，
// 只取 topK 条的话，前面被过滤掉几条，最终结果就不足 topK 了。
const candidateMultiplier = 4

// Hit 是一次检索命中。
type Hit struct {
	Doc   *schema.Document
	Score float64
}

// Filter 描述检索侧的元数据过滤条件（ACL 等）。
type Filter struct {
	Visibility string // 为空表示不过滤
	Owner      string // 为空表示不过滤
}

func (f Filter) allow(d *schema.Document) bool {
	if f.Visibility != "" && MetaString(d, constant.MetaVisibility) != f.Visibility {
		return false
	}
	if f.Owner != "" && MetaString(d, constant.MetaOwner) != f.Owner {
		return false
	}
	return true
}

// KeywordIndex 是词法通道背后的检索索引（Elasticsearch / BM25）。
//
// 这里只声明检索能力：索引的写入属于索引链路（internal/application/knowledge
// 的 Indexer 在分块落库后写），检索侧不该也不需要拿到写权限。
//
// nil 表示 es.address 没配，此时词法通道回落到 PostgreSQL 子串匹配 ——
// 功能不缺，只是没有词频、IDF 与长度归一化。
type KeywordIndex interface {
	SearchChunks(ctx context.Context, query string, topK int) ([]es.ChunkHit, error)
}

type Store struct {
	PgStore     postgres.Store
	MilvusStore milvus.Store

	// Keyword 是词法通道的检索索引，可为 nil（见 KeywordIndex 的说明）。
	Keyword KeywordIndex
}

func NewStore(ctx context.Context, entClient *ent.Client, cfg *config.Config, keyword KeywordIndex) (*Store, error) {
	pgStore := postgres.NewPostgres(entClient)
	milvusStore, err := milvus.NewMilvusStore(ctx, &milvus.MilvusConfig{
		Address:    cfg.Milvus.Address,
		Collection: cfg.Milvus.Collection,
		Dimensions: cfg.Embedding.Dimensions,
		MetricType: cfg.Milvus.MetricType,
	})
	if err != nil {
		return nil, err
	}
	return &Store{
		PgStore:     pgStore,
		MilvusStore: milvusStore,
		Keyword:     keyword,
	}, nil
}

func (s *Store) Add(ctx context.Context, docs []*schema.Document, vecs [][]float64) error {
	// [优化] 1) 先存向量再存 chunk，避免 chunk 存储失败导致向量丢失；2) 幂等：同一 source 重新入库前先清理旧向量
	//
	// 检索索引（ES）不在这里写：分块的可搜索副本由索引链路写入
	// （internal/application/knowledge 的 Indexer），那里才有 document_id /
	// dataset_id / visibility 这些归属字段的权威值。这条直连路径（cmd/ragserver
	// 的演示链）只维护 PostgreSQL 与向量库。
	_, err := s.PgStore.CreateChunk(ctx, docs)
	if err != nil {
		return err
	}
	var chunkIDs []int64
	for _, doc := range docs {
		chunkID, _ := strconv.ParseInt(doc.ID, 10, 64)
		chunkIDs = append(chunkIDs, chunkID)
	}

	// [优化] 2) 向量存储失败时，chunk 已经存储成功，可能导致向量与 chunk 不一致；可按需改成硬错误
	err = s.MilvusStore.Upsert(ctx, chunkIDs, convert.Float64ToFloat32(vecs))
	if err != nil {
		return err
	}
	return nil

}

func (s *Store) DeleteBySource(ctx context.Context, source string) error {
	return nil
}

// SearchByVector 是向量通道。
//
// Milvus 那边只存了 chunk_id 与向量，一个标量字段都没有（按量过滤不了 dataset，
// 也拿不到正文），所以命中之后必须回 PostgreSQL 把正文、标题路径和归属补齐 ——
// 引用、ACL 过滤、评测判定全靠那些字段。
//
// 返回顺序沿用 Milvus 给出的相似度顺序，不做重排：调用方（RRF 融合）只用
// 名次不用分数，重排会白白丢掉服务端已经算好的距离信息。
func (s *Store) SearchByVector(ctx context.Context, vec []float64, topK int, filter Filter) ([]Hit, error) {
	if s.MilvusStore == nil {
		return nil, errors.New("rag: vector search requires a Milvus store")
	}
	if len(vec) == 0 {
		return nil, errors.New("rag: vector search requires a non-empty query vector")
	}
	if topK <= 0 {
		return nil, errors.New("rag: search topK must be greater than zero")
	}

	results, err := s.MilvusStore.Search(
		ctx,
		convert.Float64ToFloat32([][]float64{vec})[0],
		topK*candidateMultiplier,
	)
	if err != nil {
		return nil, fmt.Errorf("rag: vector search: %w", err)
	}

	ranked := make([]rankedID, 0, len(results))
	for _, result := range results {
		ranked = append(ranked, rankedID{chunkID: result.ChunkID, score: result.Score})
	}
	return s.reload(ctx, ranked, filter, "vector")
}

// rankedID 是一条通道给出的候选：分块 ID 与它在**本通道内**的名次依据。
//
// 分数只在单通道内可比（BM25 分、余弦相似度、词元命中率三者的量纲完全不同），
// 跨通道的融合只看先后顺序，所以它不参与 RRF 计算。
type rankedID struct {
	chunkID int64
	score   float64
}

// reload 按给定顺序把候选回填成检索结果。
//
// 这是两个通道共用的收尾动作，也是「ES / Milvus 都不参与正确性判定」这条
// 规则落地的地方：外部索引只提供 ID 与名次，正文、source、visibility、owner
// 一律取 PostgreSQL 的现值，ACL 过滤在这里做。索引里的可见性是写入那一刻的
// 快照，拿它判权限，文档转私有之后就成了一次越权。
//
// channel 只用于错误信息（"vector" / "bm25"），让人一眼看出是哪条通路失败的。
func (s *Store) reload(ctx context.Context, ranked []rankedID, filter Filter, channel string) ([]Hit, error) {
	if len(ranked) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(ranked))
	for _, item := range ranked {
		ids = append(ids, item.chunkID)
	}
	chunks, err := s.PgStore.ChunksByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("rag: load chunks for %s hits: %w", channel, err)
	}

	byID := make(map[uint64]*ent.DocumentChunk, len(chunks))
	for _, chunk := range chunks {
		byID[chunk.ID] = chunk
	}

	out := make([]Hit, 0, len(ranked))
	for _, item := range ranked {
		chunk, ok := byID[uint64(item.chunkID)]
		if !ok {
			// PG 里没有对应行：删除是尽力而为的，外部索引清理失败就会留下这种
			// 孤儿（向量库被删的可能是文档已删，索引被删的可能是重新切过块）。
			// 跳过而不是报错 —— 一条脏索引记录不该让整个查询失败。
			continue
		}
		hit := chunkHit(chunk, item.score)
		if !filter.allow(hit.Doc) {
			continue
		}
		out = append(out, hit)
	}
	return out, nil
}

// SearchByText 是词法通道：产品型号、产品名这类关键字查询走的就是这里。
//
// 两条实现，输出契约完全一致（按相关性降序的 []Hit），上层 RRF 融合不需要知道
// 走的是哪一条：
//
//	配了 ES     BM25 多字段检索（词频 + IDF + 长度归一化，中文走配置的分词器）
//	没配 ES     PostgreSQL 子串匹配（见 searchBySubstring 的取舍说明）
//
// 两者的分数口径不同，但 RRF 只消费名次不消费绝对值，所以换实现不会改变融合
// 结果的结构 —— 这正是当初把「通道」和「通道的实现」分开的目的。
func (s *Store) SearchByText(ctx context.Context, query string, topK int, filter Filter) ([]Hit, error) {
	if topK <= 0 {
		return nil, errors.New("rag: search topK must be greater than zero")
	}
	candidateK := topK * candidateMultiplier
	if s.Keyword != nil {
		return s.searchByBM25(ctx, query, candidateK, filter)
	}
	return s.searchBySubstring(ctx, query, candidateK, filter)
}

// searchByBM25 用检索索引做 BM25 检索。
//
// 查询串原样交给 ES，不在本地切词：怎么切由索引侧的分词器决定，客户端再切
// 一遍只会让「查询词怎么被理解」出现两套规则。
func (s *Store) searchByBM25(ctx context.Context, query string, candidateK int, filter Filter) ([]Hit, error) {
	hits, err := s.Keyword.SearchChunks(ctx, query, candidateK)
	if err != nil {
		return nil, fmt.Errorf("rag: bm25 search: %w", err)
	}

	ranked := make([]rankedID, 0, len(hits))
	for _, hit := range hits {
		ranked = append(ranked, rankedID{chunkID: hit.ChunkID, score: hit.Score})
	}
	return s.reload(ctx, ranked, filter, "bm25")
}

// searchBySubstring 是没配 ES 时的词法通道：按词元在正文/标题路径/文档
// source 上做子串匹配。
//
// 它不依赖外部分词器：中文按 2-gram 切，拉丁字母与数字按连续段切（见 Tokenize）。
// 3-gram 以上会让中文查询完全命中不到，而 2-gram 是「不漏」的超集策略 —— 精度
// 交给后面的 RRF 与向量通道，这里只负责把可能相关的候选捞出来。
//
// 它和 BM25 的差距在「怎么排序」：这里只有「命中了几成词元」，没有词频、没有
// IDF、没有长度归一化，所以长文档容易靠「什么都沾一点」挤到前面。这是 ES 没配
// 时的降级路径，不是等价替代。
func (s *Store) searchBySubstring(ctx context.Context, query string, candidateK int, filter Filter) ([]Hit, error) {
	terms := Tokenize(query)
	if len(terms) == 0 {
		return nil, nil
	}

	chunks, err := s.PgStore.SearchChunks(ctx, terms, candidateK)
	if err != nil {
		return nil, fmt.Errorf("rag: text search: %w", err)
	}

	type scoredHit struct {
		hit     Hit
		matches int
	}
	ranked := make([]scoredHit, 0, len(chunks))
	for _, chunk := range chunks {
		hit := chunkHit(chunk, 0)
		if !filter.allow(hit.Doc) {
			continue
		}
		matches := countTermMatches(hit, chunk, terms)
		if matches == 0 {
			continue
		}
		// 分数只表示「命中了几成词元」，用于同通道内定序。RRF 不消费它的绝对值。
		hit.Score = float64(matches) / float64(len(terms))
		ranked = append(ranked, scoredHit{hit: hit, matches: matches})
	}

	// 命中词元数降序；同分按 chunk ID 升序。
	// 定序必须完全由输入决定：评测要对比两次运行的结果，随机顺序会让 diff 失去意义。
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].matches != ranked[j].matches {
			return ranked[i].matches > ranked[j].matches
		}
		return rankLess(ranked[i].hit.Doc.ID, ranked[j].hit.Doc.ID)
	})

	out := make([]Hit, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.hit)
	}
	return out, nil
}

// chunkHit 把一行 document_chunk 还原成检索结果。
//
// 元数据以 PG 的当前行为准重建，而不是直接沿用 chunk.Metadata：那一列是入库时
// 写下的快照，文档改名、改可见性或转私有之后它就过期了。source / visibility /
// owner 一律取关联 document 的现值 —— ACL 判断按旧快照判就等于越权。
func chunkHit(chunk *ent.DocumentChunk, score float64) Hit {
	meta := CopyMeta(chunk.Metadata)
	if doc := chunk.Edges.Document; doc != nil {
		meta[constant.MetaSource] = doc.Source
		meta[constant.MetaTitle] = doc.Title
		meta[constant.MetaDocID] = doc.ID
		meta[constant.MetaVisibility] = string(doc.Visibility)
		meta[constant.MetaOwner] = doc.OwnerSubject
		meta[metaKeyDatasetID] = doc.DatasetID
	}
	meta[constant.MetaChunkIndex] = chunk.ChunkIndex
	if chunk.HeadingPath != nil {
		meta[constant.MetaHeadingPath] = *chunk.HeadingPath
	}
	return Hit{
		Doc: &schema.Document{
			ID:       strconv.FormatUint(chunk.ID, 10),
			Content:  chunk.Content,
			MetaData: meta,
		},
		Score: score,
	}
}

// countTermMatches 统计有多少个词元在分块的任一可搜索面上出现过。
func countTermMatches(hit Hit, chunk *ent.DocumentChunk, terms []string) int {
	haystack := strings.ToLower(strings.Join([]string{
		chunk.Content,
		MetaString(hit.Doc, constant.MetaHeadingPath),
		MetaString(hit.Doc, constant.MetaSource),
		MetaString(hit.Doc, constant.MetaTitle),
	}, "\n"))

	matched := 0
	for _, term := range terms {
		if strings.Contains(haystack, term) {
			matched++
		}
	}
	return matched
}

// rankLess 按 chunk ID 的数值大小比较，避免 "10" < "9" 这类字典序错位。
func rankLess(a, b string) bool {
	ai, aerr := strconv.ParseInt(a, 10, 64)
	bi, berr := strconv.ParseInt(b, 10, 64)
	if aerr != nil || berr != nil {
		return a < b
	}
	return ai < bi
}

// maxPhraseRunes 是「整串也算一个词元」的长度上限。
//
// 超过这个长度整串不可能作为子串命中，只会白白多带一个 LIKE 条件。
const maxPhraseRunes = 32

// Tokenize 把查询切成用于子串匹配的词元。
//
// 规则（不依赖外部词典，中英混排一视同仁）：
//   - 拉丁字母与数字：连续段作为整体（型号 H11、H105P 这类必须整体匹配）
//   - 汉字：2-gram 滑动窗口（「滑入式铰链」→ 滑入/入式/式铰/铰链）
//   - 整串：长度不超过 maxPhraseRunes 时也作为一个词元，让完整短语命中权重更高
//
// 含 SQL LIKE 通配符的词元会被丢弃：`_` 和 `%` 在 LIKE 里不是字面量，放进去
// 会变成无意义的宽匹配。
func Tokenize(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	seen := make(map[string]struct{}, len(query))
	out := make([]string, 0, len(query))
	add := func(term string) {
		if term == "" || strings.ContainsAny(term, "%_\\") {
			return
		}
		if _, exists := seen[term]; exists {
			return
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}

	if runes := []rune(query); len(runes) <= maxPhraseRunes {
		add(strings.ToLower(query))
	}

	var latin, han []rune
	flushHan := func() {
		switch len(han) {
		case 0:
		case 1:
			add(string(han))
		default:
			for i := 0; i+1 < len(han); i++ {
				add(string(han[i : i+2]))
			}
		}
		han = han[:0]
	}
	flushLatin := func() {
		if len(latin) > 0 {
			add(string(latin))
			latin = latin[:0]
		}
	}

	for _, r := range query {
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			flushHan()
			latin = append(latin, unicode.ToLower(r))
		default:
			flushHan()
			flushLatin()
		}
	}
	flushHan()
	flushLatin()

	return out
}

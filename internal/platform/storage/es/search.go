package es

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/elastic/go-elasticsearch/v9/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/textquerytype"
)

// ChunkHit 是一条按 BM25 命中的分块。
//
// 只带 chunk ID 和分数：正文、source、归属一律回 PostgreSQL 取现值
// （见包注释），分数只用于同一次查询内定名次，跨查询没有可比性。
type ChunkHit struct {
	ChunkID int64
	Score   float64
}

// SearchChunks 用 BM25 在映射声明的字段上做多字段检索。
//
// 查询串走原样传递，不做本地切词：怎么切是索引侧分词器的口径，客户端自己再
// 切一遍只会让「查询词怎么被理解」出现两套规则。这也是引入 ES 的主要收益 ——
// 现有的 PostgreSQL 子串匹配必须自己造词元（2-gram），命中是「包含关系」，
// 没有词频、IDF 和长度归一化，长文档会靠「什么都沾一点」排到前面。
//
// 字段与权重来自映射（Mapping.SearchFields），不是本地常量：权重改了必须和
// mapping 一起改，分开写的话「字段建了但没参与打分」不会有任何提示。
//
// 取候选用的是 best_fields + or 语义：任一字段命中就算候选，最终名次由各字段
// 的最高 BM25 分决定。精度交给后面的融合与向量通道，这一路只负责把可能相关的
// 候选捞出来。
func (c *Client) SearchChunks(ctx context.Context, query string, topK int) ([]ChunkHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errEmptyQuery
	}
	if topK <= 0 {
		return nil, errors.New("es: topK 必须大于 0")
	}

	size := topK
	bestFields := textquerytype.Bestfields
	resp, err := c.Search().Index(c.index).Request(&search.Request{
		Size: &size,
		// 命中只需要 _id（= chunk_id），正文与归属回 PostgreSQL 取。
		// 关掉 _source 能省掉一次把上千字正文拉回来的传输。
		Source_: false,
		Query: &types.Query{
			MultiMatch: &types.MultiMatchQuery{
				Query:  query,
				Fields: c.mapping.SearchFields(),
				Type:   &bestFields,
			},
		},
	}).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("es: BM25 查询: %w", err)
	}
	return c.chunkHits(resp.Hits.Hits)
}

// SearchExact 只在**结构化字段**上做逐字精确匹配，是关键词之外的第三条通道。
//
// 与 SearchChunks 的分工：那一条回答「哪段文本提到了这些词」，这一条回答「哪个
// 产品的型号/系列/品类**正好等于**这些词」。产品型录里型号只出现在 YAML 头，
// 正文一次都不出现，两种查询打的是完全不同的目标，混在一条通道里就只能靠调
// 权重去挤 —— 分开之后各自有自己的权重与候选上限（见配置的 retrieval 段）。
//
// 逐字相等要求 term 查询，而不是分词后的 match：term 打在 .keyword 子字段上，
// 那个子字段存的是原样的整值，只有完全相等才命中。字段表来自映射的
// Mapping.ExactFields，没声明 keyword 的字段不会出现在这里。
//
// 查询串先按空白与标点切成词，再对每个词做一遍。这样「H105P 铰链」这种型号
// 混在句子里的输入也能命中 model=H105P 的那一条 —— 整串去比是比不中的。
func (c *Client) SearchExact(ctx context.Context, query string, topK int) ([]ChunkHit, error) {
	if topK <= 0 {
		return nil, errors.New("es: topK 必须大于 0")
	}
	fields := c.mapping.ExactFields()
	if len(fields) == 0 {
		// 映射里一个 keyword 子字段都没声明：这条通道没有可打的字段。
		// 返回空而不是报错 —— 它是可选通道，缺字段不该让整次检索失败。
		return nil, nil
	}

	terms := exactTerms(query)
	if len(terms) == 0 {
		return nil, errEmptyQuery
	}

	should := make([]types.Query, 0, len(terms)*len(fields))
	for _, term := range terms {
		for _, field := range fields {
			boost := float32(field.Boost)
			should = append(should, types.Query{
				Term: map[string]types.TermQuery{
					field.Name: {Value: term, Boost: &boost},
				},
			})
		}
	}

	size := topK
	resp, err := c.Search().Index(c.index).Request(&search.Request{
		Size:    &size,
		Source_: false,
		Query: &types.Query{
			// should 的默认 minimum_should_match 是 1：任一字段等于任一词即命中。
			Bool: &types.BoolQuery{Should: should},
		},
	}).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("es: 精确匹配查询: %w", err)
	}
	return c.chunkHits(resp.Hits.Hits)
}

// maxExactTerms 限制一次精确匹配最多带几个词。
//
// 切词是为了让型号混在句子里也能命中，但每个词都要对每个结构化字段发一个
// 子句，子句数按「词数 × 字段数」增长。长查询（一句话几十个字）会把这个乘积
// 推得很大，而长查询本来就不指望靠逐字相等来命中 —— 那种情况该由 BM25 通道
// 处理。取前几个词就够覆盖「型号 + 几个修饰词」这种实际输入。
const maxExactTerms = 8

// exactTerms 把查询切成用于逐字比较的词。
//
// 只按非字母数字的边界切（空白、标点、连字符），不做中文 2-gram：结构化字段的
// 值是「图冠系列」「铰链」这种完整词，拆成 2-gram 之后「铰链」会退化成「铰」
// 和「链」，反而比不中任何东西。这与 rag 包里那个用于子串匹配的 Tokenize 是
// 两个不同用途的切法，不要合并。
func exactTerms(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	seen := make(map[string]struct{}, maxExactTerms)
	out := make([]string, 0, maxExactTerms)

	var current []rune
	flush := func() {
		if len(current) == 0 {
			return
		}
		term := string(current)
		current = current[:0]
		if _, exists := seen[term]; exists {
			return
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}

	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			current = append(current, r)
			continue
		}
		flush()
	}
	flush()

	if len(out) > maxExactTerms {
		out = out[:maxExactTerms]
	}
	return out
}

// chunkHits 把 ES 的命中列表解析成 chunk 命中。
//
// 两段查询共用：_id 是分块 ID，解析不出数字说明索引里混进了不是本服务写入的
// 文档，跳过而不是报错 —— 一个脏文档不该让整次检索失败。
func (c *Client) chunkHits(hits []types.Hit) ([]ChunkHit, error) {
	out := make([]ChunkHit, 0, len(hits))
	for _, hit := range hits {
		if hit.Id_ == nil {
			continue
		}
		chunkID, err := strconv.ParseInt(strings.TrimSpace(*hit.Id_), 10, 64)
		if err != nil {
			continue
		}
		score := 0.0
		if hit.Score_ != nil {
			score = float64(*hit.Score_)
		}
		out = append(out, ChunkHit{ChunkID: chunkID, Score: score})
	}
	return out, nil
}

// errEmptyQuery 在查询串为空时返回。
//
// 空查询在 ES 里等价于 match_all，会把整个索引的前 topK 条捞出来当结果 ——
// 那不是「没搜到」，是「搜到了不该搜的」，所以必须在客户端就挡掉。
var errEmptyQuery = errors.New("es: 查询串为空")

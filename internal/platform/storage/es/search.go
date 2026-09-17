package es

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

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
// 的最高 BM25 分决定。精度交给后面的 RRF 融合与向量通道，这一路只负责把可能
// 相关的候选捞出来。
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

	hits := make([]ChunkHit, 0, len(resp.Hits.Hits))
	for _, hit := range resp.Hits.Hits {
		if hit.Id_ == nil {
			continue
		}
		chunkID, err := strconv.ParseInt(strings.TrimSpace(*hit.Id_), 10, 64)
		if err != nil {
			// 索引里混进了不是本服务写入的文档（_id 不是分块 ID）。
			// 跳过而不是报错：一个脏文档不该让整次检索失败。
			continue
		}
		score := 0.0
		if hit.Score_ != nil {
			score = float64(*hit.Score_)
		}
		hits = append(hits, ChunkHit{ChunkID: chunkID, Score: score})
	}
	return hits, nil
}

// errEmptyQuery 在查询串为空时返回。
//
// 空查询在 ES 里等价于 match_all，会把整个索引的前 topK 条捞出来当结果 ——
// 那不是「没搜到」，是「搜到了不该搜的」，所以必须在客户端就挡掉。
var errEmptyQuery = errors.New("es: 查询串为空")

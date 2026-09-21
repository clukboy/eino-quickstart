// Package es 封装关键词检索用的 Elasticsearch。
//
// 它在链路里只出现两次：
//
//	写入  knowledge.Indexer / rag.Store.Add   分块 -> 检索文档（BM25 的可搜索副本）
//	查询  rag.Store.SearchBy*                  query -> 按名次排好的 chunk ID
//
// 查询侧有两条通道共用这个索引，它们回答不同的问题：
//
//	SearchChunks  分词后的 BM25：「哪段文本提到了这些词」（含正文与兜底字段）
//	SearchExact   结构化字段逐字相等：「哪个产品的型号/系列/品类正好是这个」
//
// 有一条边界必须守住：**ES 不参与正确性判定**。它给出的只是名次，命中的分块
// 还要回 PostgreSQL 取正文、source、visibility、owner，ACL 过滤与引用格式都
// 以 PostgreSQL 的现值为准。理由是索引里的 visibility / owner 是写入那一刻的
// 快照，文档转私有或改归属之后它就过期了，拿快照判权限等于越权。所以这里的
// 查询不接收 ACL 条件，只在候选名次上做文章。
//
// 「地址为空」是一个合法状态：New 返回 nil，调用方据此让词法通道回落到
// PostgreSQL 子串匹配。ES 是检索质量的增强，不是系统的单点。
package es

import (
	"context"
	"fmt"
	"os"
	"strings"

	"eino-quickstart/internal/platform/config"

	"github.com/elastic/go-elasticsearch/v9"
	"github.com/elastic/go-elasticsearch/v9/typedapi/core/count"
	"github.com/elastic/go-elasticsearch/v9/typedapi/indices/putmapping"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
)

type Client struct {
	*elasticsearch.TypedClient

	index    string
	analyzer string
	bulkSize int

	// mapping 是这个索引的检索面声明，从 es.mappingFile 加载（见 mapping.go）。
	// 它同时决定 建索引的属性表、查询的字段权重表、写入时的取值规则。
	mapping *Mapping

	// rev 是 mapping 的形态指纹，写入每份文档时盖章。预检靠它识别「更早的
	// 映射写下的文档」，从而把「升级后按型号搜不到」和「BM25 没用」区分开。
	rev string
}

const analyzerHintUnknown = "\n提示：ik_max_word / ik_smart 需要集群安装 IK 插件；" +
	"没装插件请把 es.analyzer 改成内置的 cjk（并按 es.index 的注释换一个版本号）"

func New(cfg *config.ESConfig) (*Client, error) {
	if cfg == nil || !cfg.Enabled() {
		return nil, nil
	}

	// 映射先加载：它是纯本地文件，解析失败就没必要再去连集群。顺序反过来会
	// 让「配置写错了」表现为一个超时或 401，排查方向完全不同。
	mappingPath := strings.TrimSpace(cfg.MappingFile)
	if mappingPath == "" {
		return nil, fmt.Errorf(
			"es: 配了 es.address 就必须给 es.mappingFile\n" +
				"检索面（字段名、类型、权重、从元数据的哪个路径取值）由映射文件定义，" +
				"没有它既建不出索引，也无法决定写入哪些字段\n" +
				"可参考 configs/es/chunk_mapping.json",
		)
	}
	mapping, err := LoadMapping(mappingPath)
	if err != nil {
		return nil, err
	}

	opts := []elasticsearch.Option{
		elasticsearch.WithAddresses(cfg.Address...),
		elasticsearch.WithRetry(2, 502, 503, 504),
	}
	if cfg.Username != "" {
		opts = append(opts, elasticsearch.WithBasicAuth(cfg.Username, cfg.Password))
	}
	if cfg.CaCertPath != "" {
		cert, err := os.ReadFile(cfg.CaCertPath)
		if err != nil {
			return nil, fmt.Errorf("es: 读取 CA 证书 %q: %w", cfg.CaCertPath, err)
		}
		opts = append(opts, elasticsearch.WithCACert(cert))
	}

	client, err := elasticsearch.NewTyped(opts...)
	if err != nil {
		return nil, fmt.Errorf("es: 构造客户端: %w", err)
	}
	return &Client{
		TypedClient: client,
		index:       cfg.Index,
		analyzer:    cfg.Analyzer,
		bulkSize:    cfg.BulkSize,
		mapping:     mapping,
		rev:         mapping.fingerprint(cfg.Analyzer),
	}, nil
}

// Index 返回分块索引名。
func (c *Client) Index() string { return c.index }

// MappingName 返回当前映射的名字，供日志与报错指明「用的是哪份映射」。
func (c *Client) MappingName() string {
	if c == nil || c.mapping == nil {
		return ""
	}
	return c.mapping.Name
}

// Searcher 是检索侧要注入的能力，与 rag.KeywordIndex 的方法集一致。
//
// 两条方法对应两条独立的关键词通道：SearchChunks 是分词后的 BM25，
// SearchExact 是结构化字段上的逐字相等。它们在检索策略里各有各的权重与候选
// 上限，所以是两条方法而不是一条带开关的方法。
type Searcher interface {
	SearchChunks(ctx context.Context, query string, topK int) ([]ChunkHit, error)
	SearchExact(ctx context.Context, query string, topK int) ([]ChunkHit, error)
}

// Writer 是索引侧要注入的能力，与 knowledge.KeywordIndex 的方法集一致。
type Writer interface {
	IndexChunks(ctx context.Context, docs []ChunkDoc) error
	DeleteByDocument(ctx context.Context, documentID uint64) (int64, error)
}

func (c *Client) Searcher() Searcher {
	if c == nil {
		return nil
	}
	return c
}

func (c *Client) Writer() Writer {
	if c == nil {
		return nil
	}
	return c
}

func (c *Client) Health(ctx context.Context) error {
	if _, err := c.TypedClient.Ping().Do(ctx); err != nil {
		return fmt.Errorf("es: 集群不可达: %w", err)
	}
	return nil
}

// EnsureIndex 确保分块索引存在、分词器与配置一致、索引里有当前映射的字段。
//
// 索引不存在就按当前映射建；已存在则校验分词器（不能就地改，只能报错）并把
// 映射新增的字段补写上去（可以就地加，不必重建）。
func (c *Client) EnsureIndex(ctx context.Context) error {
	properties, err := c.mapping.properties(c.analyzer)
	if err != nil {
		return err
	}

	exists, err := c.Indices.Exists(c.index).Do(ctx)
	if err != nil {
		return fmt.Errorf("es: 检查索引 %q: %w", c.index, err)
	}
	if !exists {
		if _, err := c.Indices.Create(c.index).
			Mappings(&types.TypeMapping{
				Properties: properties,
				Dynamic:    &indexDynamic,
			}).
			Do(ctx); err != nil {
			hint := ""
			if strings.Contains(strings.ToLower(err.Error()), "analyzer") {
				hint = analyzerHintUnknown
			}
			return fmt.Errorf(
				"es: 创建索引 %q（analyzer=%s）失败: %w%s",
				c.index, c.analyzer, err, hint,
			)
		}
		return nil
	}
	if err := c.verifyAnalyzer(ctx); err != nil {
		return err
	}
	return c.ensureMapping(ctx, properties)
}

// ensureMapping 把当前映射的字段补写到已有索引上。
//
// 用 PutMapping 而不是「版本变了就报错」，是因为加字段是 ES 明确支持的原地
// 操作，逼运维去换索引名重建、再重跑全部文档，代价高得不成比例。真正需要
// 换索引名的是字段**类型**变了，那种情况 PutMapping 会失败，下面补一句能
// 照做的提示。
//
// 动态映射策略也一并写回：老索引可能是 dynamic=true 时建的，不纠正的话，
// 映射文件里漏声明的字段会被 ES 悄悄加成 standard 分词的 text。
func (c *Client) ensureMapping(ctx context.Context, properties map[string]types.Property) error {
	if _, err := c.Indices.PutMapping(c.index).Request(&putmapping.Request{
		Properties: properties,
		Dynamic:    &indexDynamic,
	}).Do(ctx); err != nil {
		return fmt.Errorf("es: 补写索引 %q 的 mapping: %w%s", c.index, err, mappingConflictHint(err))
	}
	return nil
}

// mappingConflictHint 在字段类型冲突时补一句提示。
//
// ES 的错误信息（"mapper [model] cannot be changed from type [keyword] to
// [text]"）说清了现象，但没说怎么办 —— 而唯一的出路是换个索引名重建。
func mappingConflictHint(err error) string {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "cannot be changed") || strings.Contains(message, "conflicts with") {
		return "\n提示：已有字段的类型与新 mapping 冲突，ES 不允许就地改字段类型；" +
			"请把 es.index 换成新的版本号（如 _v2）后重新索引"
	}
	return ""
}

// verifyAnalyzer 校验已存在的索引里 content 字段实际用的分词器与配置一致。
//
// 这不是洁癖。ES 的 mapping 不能就地改分词器，所以「改了 es.analyzer 却忘了
// 换 es.index」的后果是：进程照常启动、写入照常成功、检索命中率悄悄变差。
// 排查时会一路怀疑到 embedding、切块策略和阈值上，很难想到根因是索引里的
// 分词器没换。在启动时直接报出来，这个坑就只有一次。
func (c *Client) verifyAnalyzer(ctx context.Context) error {
	resp, err := c.Indices.GetMapping().Index(c.index).Do(ctx)
	if err != nil {
		return fmt.Errorf("es: 读取索引 %q 的 mapping: %w", c.index, err)
	}
	// 响应按索引名索引；别名或通配符命中的情况拿不到确定条目，跳过校验。
	record, ok := resp[c.index]
	if !ok {
		return nil
	}
	// Property 在生成代码里声明成 any，但库为具体属性类型提供了自定义解码，
	// 解出来的是 *types.TextProperty。取不到 text 属性就说明这个字段不是按
	// 本服务的 mapping 建的（比如被人工改成了 keyword），同样不在这里判定。
	property, ok := record.Mappings.Properties[fieldContent].(*types.TextProperty)
	if !ok || property.Analyzer == nil {
		return nil
	}
	actual := *property.Analyzer
	if actual == c.analyzer {
		return nil
	}
	return fmt.Errorf(
		"es: 索引 %q 的 %s 字段分词器是 %q，配置里是 %q；mapping 的分词器不能就地修改，"+
			"请把 es.index 换成新的版本号（如 _v2）后重新索引",
		c.index, fieldContent, actual, c.analyzer,
	)
}

// DocCount 返回索引里的检索文档数。
//
// 它只服务于预检：索引存在但一条文档都没有时，关键词通道必然返回空结果，
// 那种「跑了全是 0」的结果最容易被误读成召回坏了。
//
// 名字不叫 Count 同样是躲开内嵌客户端上的同名方法。
func (c *Client) DocCount(ctx context.Context) (int64, error) {
	resp, err := c.TypedClient.Count().Index(c.index).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("es: 统计索引 %q 文档数: %w", c.index, err)
	}
	return resp.Count, nil
}

// StaleDocs 返回索引里形态指纹与当前映射不一致的文档数。
//
// 覆盖三类：本次改动之前写入的（根本没有这个字段）、映射改过之后没回填的，
// 以及从别的索引复制过来的。它们缺的是当前映射声明的检索面 —— 按型号、系列、
// 品类搜不到它们，但这**不是** BM25 失效，是文档还没回填。区分这两件事是它
// 存在的全部意义：不区分的话，升级之后第一次评测会得出「加了 ES 反而更差」
// 的结论。
//
// 指纹只覆盖索引期形态，所以调权重不会让文档变陈（见 Mapping.fingerprint）。
//
// 回填方式是对文档触发 reindex（POST /api/v1/dataset/:id/documents/:docId/reindex），
// 或重跑 worker 的索引任务。
func (c *Client) StaleDocs(ctx context.Context) (int64, error) {
	resp, err := c.TypedClient.Count().Index(c.index).Request(&count.Request{
		Query: &types.Query{
			Bool: &types.BoolQuery{
				// 「指纹等于当前值」取反 = 指纹不同的 + 没有指纹的。
				MustNot: []types.Query{
					{Term: map[string]types.TermQuery{
						fieldMappingRev: {Value: c.rev},
					}},
				},
			},
		},
	}).Do(ctx)
	if err != nil {
		return 0, fmt.Errorf("es: 统计索引 %q 里的陈旧文档: %w", c.index, err)
	}
	return resp.Count, nil
}

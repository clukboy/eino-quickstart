package knowledge

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"eino-quickstart/ent"
	"eino-quickstart/internal/platform/observability"
	"eino-quickstart/internal/rag/grouping"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// ErrSearchUnavailable 表示这个进程没有装配召回能力。
//
// 它是「部署形态」而不是「请求错了」：worker 进程只消费索引任务、不做召回，
// 所以它的知识库服务没有检索器。传输层应当把它映射成 503（依赖缺失），而不是
// 400 或 500 —— 调用方重试没用，但运维一眼知道是装配问题。
var ErrSearchUnavailable = errors.New("knowledge: retrieval is not wired into this process")

// Searcher 是召回侧的窄端口。
//
// 和这个包里其他端口一样，它只描述「给定查询与范围，拿回命中的分块」，不含
// 「有几条通道、各自多重、候选取多少、怎么融合」—— 那些是检索实现的事。换向量
// 库、关掉向量通道、甚至整体换成别的检索服务，都不该改动用例层。
type Searcher interface {
	Search(ctx context.Context, query string, scope SearchScope) (SearchOutcome, error)
}

// SearchScope 是一次召回的检索范围。
type SearchScope struct {
	// DatasetID 限定在哪个数据集里召回。0 表示不限（只有明确需要跨库检索的
	// 调用方才该这么用）。
	DatasetID uint64
	// TopK 是期望的结果条数，<=0 交给检索实现的默认值。
	TopK int
}

// SearchHit 是一条召回命中。
//
// 检索实现返回的它**是分块**：召回回答的是「哪一段文字能回答这个问题」，同一篇
// 文档可能命中多个段落。但 Service.Search 的返回值会按数据集类型归并（见
// SearchOutcome.Granularity）—— 产品型录一篇文档就是一个产品，把同一篇文档的
// 八个命中块原样喂给下游，换来的只是重复内容和被挤掉的别的内容。
type SearchHit struct {
	ChunkID     uint64
	DocumentID  uint64
	Source      string
	Title       string
	HeadingPath string
	Content     string
	Score       float64
}

// SearchOutcome 是一次召回的完整结果。
type SearchOutcome struct {
	// Hits 是召回结果，顺序即相关性顺序。它的粒度由 Granularity 决定：
	// document 粒度下一篇文章一条（取命中的最高分块作代表），chunk 粒度下
	// 一个分块一条。
	Hits []SearchHit

	// TopK 是本次实际用的条数（收敛到配置区间之后的值）。
	//
	// 单独回传它而不是让调用方回显自己传的值：请求 100 条而配置上限是 10 时，
	// 响应里说 100 会让调用方以为「只找到 10 条」，而实际是「只允许要 10 条」。
	TopK int

	// Granularity 是 Hits 的归并粒度，由数据集类型决定（knowledge.recallGrouping）。
	//
	// 必须回传：TopK 是「条数」上限，而「条」在两种粒度下不是同一个东西 ——
	// 同一个 top_k=5，chunk 粒度下可能只覆盖 1 篇文档。调用方只看条数会以为
	// 要五篇给了五条。
	Granularity grouping.Granularity

	// Channels 是真正给出了候选的通道，按优先级排列（精确 / 关键词 / 向量）。
	Channels []string

	// Degraded 是降级的通道：本次结果不含它们的贡献。为空表示结果是完整的；
	// 非空表示少了一条通道，条数偏少或某个型号搜不到可能就是它造成的 ——
	// 没有这个信息时，「通道挂了」和「确实没有相关内容」在看结果的人眼里
	// 完全一样，而这两者的下一步动作完全不同。
	Degraded []string
}

// Limits 是召回入口的输入约束，对应配置的 knowledge 段。
//
// 约束放在用例层而不是传输层：它们是业务规则（一次给多少条、问题能问多长），
// 换一个入口（对话侧的工具调用、批处理）时同样要守，写在 handler 里就会被绕过。
type Limits struct {
	DefaultTopK        int
	MaxTopK            int
	MaxQueryCharacters int
}

// withDefaults 补上缺省值。
//
// 它同时保证 DefaultTopK 不超过 MaxTopK：配置写反时（默认 10、上限 5）如果不
// 收敛，表现是「不传 top_k 比传 top_k 拿到的还多」，一个自相矛盾的接口。
func (l Limits) withDefaults() Limits {
	if l.DefaultTopK <= 0 {
		l.DefaultTopK = 5
	}
	if l.MaxTopK <= 0 || l.MaxTopK < l.DefaultTopK {
		l.MaxTopK = l.DefaultTopK
	}
	if l.MaxQueryCharacters <= 0 {
		l.MaxQueryCharacters = 2000
	}
	return l
}

// clampTopK 把调用方要的条数收敛到允许区间。
func (l Limits) clampTopK(requested int) int {
	if requested <= 0 {
		return l.DefaultTopK
	}
	if requested > l.MaxTopK {
		return l.MaxTopK
	}
	return requested
}

// SearchInput 是一次召回的入参。
type SearchInput struct {
	DatasetID uint64
	Query     string
	TopK      int
}

// Search 在某个数据集内召回与 query 相关的分块。
//
// 这里只做三件事：校验参数、确认数据集存在、把范围交给检索实现。三条通道怎么
// 分工、哪条降级了，都由 SearchOutcome 带回来，用例层不替检索实现做决定。
//
// 关于可见性：这一层**不按属主收窄**。数据集就是这一层的授权边界（能进这个
// 数据集的人能看到它的内容），而 private 文档的「谁能看」要结合主体身份判断，
// 那是对话侧工具（按主体的数据集白名单检索）该做的事 —— 在这里按 owner 过滤
// 会把 system 可见的文档（属主是上传者而不是提问者）一起挡掉，那是漏召。
//
// 「数据集不存在」返回 ErrDatasetNotFound 而不是空结果：空结果看起来和「这个
// 数据集里没有相关内容」一模一样，而调用方的下一步动作完全不同（检查 id 还是
// 换个说法再问）。
func (s *Service) Search(ctx context.Context, in SearchInput) (_ SearchOutcome, err error) {
	ctx, span := observability.StartSpan(ctx, "knowledge.search",
		oteltrace.WithAttributes(
			attribute.Int64("knowledge.dataset_id", int64(in.DatasetID)),
		),
	)
	defer func() {
		observability.SpanError(span, err)
		span.End()
	}()

	if s.searcher == nil {
		return SearchOutcome{}, ErrSearchUnavailable
	}

	query := strings.TrimSpace(in.Query)
	if query == "" {
		return SearchOutcome{}, invalid("query is required")
	}
	// 按字符数而不是字节数限长：一个汉字占 3 个字节，按字节算会让中文提问的
	// 可用长度只有英文的三分之一，而它们是同一个「问题有多长」的概念。
	if length := utf8.RuneCountInString(query); length > s.limits.MaxQueryCharacters {
		return SearchOutcome{}, invalid(
			"query is too long: %d characters (limit %d)", length, s.limits.MaxQueryCharacters,
		)
	}

	// 取数据集实体而不是只判存在，就是为了拿到 type：同一套检索链路要同时服务
	// 「一个文档就是一个产品」的型录库和普通文档库，归并粒度只能是每个库自己说。
	dataset, err := s.dataset(ctx, in.DatasetID)
	if err != nil {
		return SearchOutcome{}, err
	}

	topK := s.limits.clampTopK(in.TopK)
	outcome, err := s.searcher.Search(ctx, query, SearchScope{
		DatasetID: in.DatasetID,
		TopK:      topK,
	})
	if err != nil {
		return SearchOutcome{}, err
	}
	// 以用例层收敛后的值为准，而不是信检索实现回填的：条数上限是这一层的
	// 约束，换一个检索实现不该让响应里的 top_k 变个说法。
	outcome.TopK = topK

	// 归并放在这一层而不是检索实现里：Searcher 的契约仍是「拿回命中的分块」，
	// 归并是用例层的决定。这样换一个检索实现（换向量库、加容器检索）不会漏掉
	// 它 —— 漏掉的表现是产品库里同一个产品重复出现，看起来像召回质量差。
	matchedChunks := len(outcome.Hits)
	outcome.Hits, outcome.Granularity = s.applyGranularity(dataset, outcome.Hits)

	span.SetAttributes(
		attribute.Int("knowledge.hits", len(outcome.Hits)),
		attribute.Int("knowledge.matched_chunks", matchedChunks),
		attribute.String("knowledge.granularity", string(outcome.Granularity)),
		attribute.Int("knowledge.top_k", topK),
		attribute.StringSlice("knowledge.channels", outcome.Channels),
		attribute.StringSlice("knowledge.degraded_channels", outcome.Degraded),
	)
	return outcome, nil
}

// applyGranularity 按知识库类型归并召回结果，并回传用了哪种粒度。
//
// 单独拎出来是为了让「粒度取自哪个数据集的哪个字段」有个可断言的地方：
// 归并算法本身好测，真正容易写错的是前面那一步 —— 拿错类型（比如用请求里的
// 某个参数）不会报错，只会让某一类库的结果条数悄悄变成另一种单位。
func (s *Service) applyGranularity(dataset *ent.Dataset, hits []SearchHit) ([]SearchHit, grouping.Granularity) {
	granularity := s.grouping.For(dataset.Type)
	return groupHits(hits, granularity), granularity
}

// groupHits 按粒度归并召回结果。
//
// document 粒度下同一篇文档只留一条，取得分最高的一块作代表 —— 分高的那块最
// 贴近查询，它的 HeadingPath 与正文最能解释「为什么命中」。chunk 粒度下原样返回。
//
// 两种粒度下**顺序都保持检索给出的相关性顺序**，不在这里按分数重排：重排是检索
// 侧的职责（reranker），在这里顺手排一遍会让「权重改动了有没有生效」变得不可
// 观测 —— 分数是融合后的值，重排之后没人看得出原来的名次是什么。
func groupHits(hits []SearchHit, granularity grouping.Granularity) []SearchHit {
	if granularity == grouping.Chunk || len(hits) < 2 {
		return hits
	}
	merged := make([]SearchHit, 0, len(hits))
	position := make(map[string]int, len(hits))
	for _, hit := range hits {
		key := strings.TrimSpace(hit.Source)
		at, seen := position[key]
		if !seen {
			position[key] = len(merged)
			merged = append(merged, hit)
			continue
		}
		if hit.Score > merged[at].Score {
			merged[at] = hit
		}
	}
	return merged
}

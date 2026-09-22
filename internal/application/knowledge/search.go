package knowledge

import (
	"context"
	"errors"
	"log/slog"
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
// 和这个包里其他端口一样，它只描述「给定查询与取数预算，拿回命中的分块」，不含
// 「有几条通道、各自多重、候选取多少、怎么融合」—— 那些是检索实现的事。换向量
// 库、关掉向量通道、甚至整体换成别的检索服务，都不该改动用例层。
//
// 它**不做归并、也不折算条数**：预算要多少块是用例层按粒度算好的（见 recall），
// 命中怎么归并成「条」也是用例层的决定。把这两件事放进检索实现，换一个实现就会
// 悄悄换掉结果的单位。
type Searcher interface {
	Search(ctx context.Context, query string, scope SearchScope) (SearchOutcome, error)
}

// SearchScope 是一次召回的检索范围与取数预算。
type SearchScope struct {
	// DatasetID 限定在哪个数据集里召回。0 表示不限（只有明确需要跨库检索的
	// 调用方才该这么用）。
	DatasetID uint64

	// ChunkBudget 是这一次要向检索侧要多少个**分块**，<=0 交给检索实现的默认值。
	//
	// 它是分块数，不是结果条数 —— 两者只在 chunk 粒度下碰巧相等。条数的单位由
	// 归并粒度决定（见 SearchOutcome.Hits），兑换率取决于切块密度，所以折算在
	// 用例层完成（见 grouping.FetchPlan）。把「要几篇」直接当分块数传下去，就是
	// 「请求 20 篇、返回 4 篇」那类汇报的成因。
	ChunkBudget int
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

	// Truncated 表示 Content 是按上限截断过的。只有 document 粒度（正文补成
	// 整篇文档）才可能为 true —— 分块粒度下每一块的体积本来就被切块器限住。
	//
	// 必须显式带出去：被截断的正文与完整的正文在调用方眼里长得一样，而下游拿它
	// 当「这篇文档的全部内容」用时，缺失的部分不会有任何痕迹。
	Truncated bool
}

// SearchOutcome 是一次召回的完整结果。
type SearchOutcome struct {
	// Hits 是召回结果，顺序即相关性顺序。它的粒度由 Granularity 决定：
	// document 粒度下一篇文章一条（取命中的最高分块作代表，正文补成整篇文档），
	// chunk 粒度下一个分块一条。
	Hits []SearchHit

	// TopK 是本次实际用的条数（收敛到配置区间之后的值）。
	//
	// 单独回传它而不是让调用方回显自己传的值：请求 100 条而配置上限是 10 时，
	// 响应里说 100 会让调用方以为「只找到 10 条」，而实际是「只允许要 10 条」。
	TopK int

	// MatchedChunks 是归并前命中的分块数。它和 len(Hits) 一起回答「为什么只有
	// 几条」—— 命中 40 块归并出 4 篇，说明库里匹配的就这 4 篇；命中 20 块归并出
	// 4 篇、而预算是 80，说明检索侧没能给满。没有这个数，两种情况的响应一模一样，
	// 而下一步动作完全不同（换个说法再问 / 去查检索侧）。
	MatchedChunks int

	// ChunkBudget 是本次实际用的分块预算（多轮放大之后的最终值）。
	//
	// 它必须与 MatchedChunks 一起看：预算远大于命中块数是「检索侧见底」的唯一
	// 证据，也是判断「切块是不是过碎、把候选名额吃光了」的依据。
	ChunkBudget int

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

	// MaxContentBytes 是单条结果正文的字节上限（对应配置的
	// knowledge.maxResultBytes）。
	//
	// document 粒度下正文是**整篇文档**，一篇长型录可能有几万字：没有上限时
	// 一次 top_k=20 的召回能带出几百 KB，把提示词预算与响应体一起打满，而这件事
	// 由调用方传一个数字就能触发。超过上限的正文会被截断并打上 Truncated 标记
	// —— 截断本身不是问题，悄悄截断才是。
	MaxContentBytes int
}

// defaultMaxContentBytes 是没配 maxResultBytes 时的正文上限。
//
// 取 32KiB：一个产品文档通常几 KB，它只会在「整篇产品手册被当成一篇文档」这类
// 情况下生效。带上限而不整体拒绝，是因为调用方要的是「内容尽量全」，不是
// 「要么全给要么不给」。
const defaultMaxContentBytes = 32768

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
	if l.MaxContentBytes <= 0 {
		l.MaxContentBytes = defaultMaxContentBytes
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

	// 粒度先定，再取数：条数的单位决定了该向检索侧要多少分块（见 recall）。
	topK := s.limits.clampTopK(in.TopK)
	granularity := s.granularityFor(dataset)

	outcome, err := s.recall(ctx, query, in.DatasetID, granularity, topK)
	if err != nil {
		return SearchOutcome{}, err
	}
	// 以用例层收敛后的值为准，而不是信检索实现回填的：条数上限是这一层的
	// 约束，换一个检索实现不该让响应里的 top_k 变个说法。
	outcome.TopK = topK

	span.SetAttributes(
		attribute.Int("knowledge.hits", len(outcome.Hits)),
		attribute.Int("knowledge.matched_chunks", outcome.MatchedChunks),
		attribute.Int("knowledge.chunk_budget", outcome.ChunkBudget),
		attribute.String("knowledge.granularity", string(outcome.Granularity)),
		attribute.Int("knowledge.top_k", topK),
		attribute.StringSlice("knowledge.channels", outcome.Channels),
		attribute.StringSlice("knowledge.degraded_channels", outcome.Degraded),
	)
	return outcome, nil
}

// recall 按归并粒度取数：反复向检索侧要分块，直到取满目标条数。
//
// 为什么是循环而不是一次拍一个大倍数：每篇文档命中几块（兑换率）事先不知道，
// 常见值 2~4 与病态值几十差一个量级。拍小了取不满（就是「请求 20 篇、返回 4 篇」），
// 拍大了每一次召回都白拉几倍候选。先按小倍数试、不够再翻倍，正常情况只跑一轮，
// 退化情况多跑一两轮，两条路都可解释（见 grouping.FetchPlan）。
//
// 返回的是**最后一次**检索的通道台账：多轮之间只有预算不同，通道状态是同一套，
// 而最后一次决定了最终交出去的内容。
func (s *Service) recall(
	ctx context.Context,
	query string,
	datasetID uint64,
	granularity grouping.Granularity,
	want int,
) (SearchOutcome, error) {
	plan := grouping.NewFetchPlan(granularity, want)

	for {
		outcome, err := s.searcher.Search(ctx, query, SearchScope{
			DatasetID:   datasetID,
			ChunkBudget: plan.Budget(),
		})
		if err != nil {
			return SearchOutcome{}, err
		}

		grouped := groupHits(outcome.Hits, granularity)
		if plan.Settled(len(grouped), len(outcome.Hits)) {
			return s.settle(ctx, outcome, grouped, granularity, plan.Budget()), nil
		}
		widened, ok := plan.Widen()
		if !ok {
			// 到顶了（Settled 在这条路上本不该为假，留着是为了不出现「同一份
			// 候选拉第二遍」的死循环）。
			return s.settle(ctx, outcome, grouped, granularity, plan.Budget()), nil
		}
		plan = widened
	}
}

// settle 把归并结果与取数台账装回 outcome，并把正文补齐到该粒度应有的样子。
func (s *Service) settle(
	ctx context.Context,
	outcome SearchOutcome,
	grouped []SearchHit,
	granularity grouping.Granularity,
	budget int,
) SearchOutcome {
	// 先记块数再覆盖 Hits：归并之后 len(Hits) 已经变成「条数」，
	// 「归并前命中几块」是解释条数为什么这么少的另一半证据。
	outcome.MatchedChunks = len(outcome.Hits)
	outcome.Hits = s.fillFullContent(ctx, grouped, granularity)
	outcome.Granularity = granularity
	outcome.ChunkBudget = budget
	return outcome
}

// fillFullContent 在按文档归并时，把代表块的正文换成整篇文档的正文。
//
// 代表块回答的是「这篇文档为什么被召回」，它是**检索**的单位，不是阅读的单位。
// 产品型录里一篇文档就是一个产品：型号、系列、规格表与描述散在同一篇的好几块里，
// 只回一块等于把一个产品拆开只给一半，下游拿不到规格 —— 而这件事在响应里看不出
// 任何异常，只是内容少了一截。
//
// 正文取自 documents.content，而不是把命中块拼起来：切块是带 overlap 的滑动
// 窗口，拼回去会把重叠部分重复一遍，同时仍然丢掉没命中的段落。也不用索引里
// 那份正文 —— ES 里存的是**分块**，它只覆盖命中的那几段。
//
// chunk 粒度下不做替换：那一条结果**就是**一个分块，换成全文等于把粒度悄悄改成
// 文档，调用方按分块做引用、去重与定位会全部错位。
func (s *Service) fillFullContent(
	ctx context.Context,
	hits []SearchHit,
	granularity grouping.Granularity,
) []SearchHit {
	if granularity != grouping.Document || len(hits) == 0 {
		return hits
	}
	contents, err := documentContents(ctx, s.contents, hits)
	if err != nil {
		// 取不到正文时保留代表块正文：它至少是文档里最贴近查询的那一段，比空着
		// 强。同时留一条日志 —— 这是运维异常，不该静默降级成「内容就是这些」。
		observability.LogWithTrace(ctx, s.logger).Warn("knowledge: 召回补整篇正文失败，退回代表块正文",
			slog.String("error", err.Error()),
		)
		return hits
	}
	for index := range hits {
		full, ok := contents[hits[index].DocumentID]
		if !ok || strings.TrimSpace(full) == "" {
			// 没有 document_id、或者那条文档的正文是空的（存量文档还没导进来）。
			// 不报错也不清空：代表块比空内容有用。
			observability.LogWithTrace(ctx, s.logger).Warn("knowledge: 命中没有可用的整篇正文，退回代表块正文",
				slog.Uint64("document_id", hits[index].DocumentID),
				slog.String("source", hits[index].Source),
			)
			continue
		}
		hits[index].Content, hits[index].Truncated = truncateContent(full, s.limits.MaxContentBytes)
	}
	return hits
}

// truncateContent 按字节上限截断正文，并报告有没有截断。
//
// 按**字节**而不是字符：上限来自配置的 maxResultBytes，配它的人是按响应体大小
// 算的。
//
// 截断点必须回退到 rune 边界：从一个汉字中间切开会得到无效 UTF-8，JSON 编码时
// 被替换成 U+FFFD，调用方看到的是一个坏字符而看不出「这里被截断了」。
func truncateContent(content string, limit int) (string, bool) {
	if limit <= 0 || len(content) <= limit {
		return content, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return content[:cut], true
}

// granularityFor 取这个数据集该用的归并粒度。
//
// 单独拎出来是为了让「粒度取自哪个数据集的哪个字段」有个可断言的地方：归并算法
// 本身好测，真正容易写错的是前面那一步 —— 拿错来源（比如用请求里的某个参数）
// 不会报错，只会让某一类库的结果条数悄悄变成另一种单位。
//
// 它在取数**之前**就要定下来：条数的单位决定了该向检索侧要多少分块。
func (s *Service) granularityFor(dataset *ent.Dataset) grouping.Granularity {
	return s.grouping.For(dataset.Type)
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

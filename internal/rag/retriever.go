package rag

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"eino-quickstart/internal/rag/constant"

	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
)

// Reranker 可选重排接口（cross-encoder / LLM rerank 均可实现）。
// [优化] 新增：召回后精排是提升 RAG 效果最显著的手段之一。
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []*schema.Document) ([]*schema.Document, error)
}

// Channel 是一条检索通道的名字。
//
// 三条通道按优先级排列：精确 > 关键词 > 向量。这个顺序不是装饰 —— 前两条都
// 只依赖 PostgreSQL 与检索索引，是「问型号答型号」这类查询的主力，而且不需要
// embedding 服务；向量通道补的是表述差异。
type Channel string

const (
	// ChannelExact 在结构化字段（型号 / 产品 ID / 系列 / 品类）上做逐字相等匹配。
	ChannelExact Channel = "exact"
	// ChannelKeyword 是分词后的 BM25 多字段检索。
	ChannelKeyword Channel = "keyword"
	// ChannelVector 是语义近邻检索。
	ChannelVector Channel = "vector"
)

// RetrievalPolicy 是三条通道怎么配合：权重、融合平滑、候选上限。
//
// 全部来自配置的 retrieval 段，不是代码里的常量：调检索质量最频繁的就是这几个
// 数，改它们不该需要重新编译。权重为 0 表示这条通道不参与（配置校验保证至少
// 有一条大于 0）；把向量权重置 0 就是纯关键词召回。
type RetrievalPolicy struct {
	ExactWeight   float64
	KeywordWeight float64
	VectorWeight  float64

	// RRFSmoothing 是 RRF 的平滑常数 k。越大，名次之间的差距被压得越平
	// （前几名与十几名的贡献接近），越小则头部名次越强势。
	RRFSmoothing int

	ExactCandidateLimit   int
	KeywordCandidateLimit int
	VectorCandidateLimit  int
}

// defaultRetrievalPolicy 是策略全零时的兜底。
//
// 只有在调用方完全不设策略时才会走到这里（真实部署的配置校验要求至少一个权重
// 大于 0），所以它只保证「能用」，不代表推荐值 —— 推荐值在 configs/config.yaml
// 的 retrieval 段里，那里也是唯一该调整它们的地方。
func defaultRetrievalPolicy() RetrievalPolicy {
	return RetrievalPolicy{
		ExactWeight: 3, KeywordWeight: 1.5, VectorWeight: 1,
		RRFSmoothing:          60,
		ExactCandidateLimit:   20,
		KeywordCandidateLimit: 30,
		VectorCandidateLimit:  30,
	}
}

// normalize 补上缺省的平滑常数与候选上限，并报告是否要给权重兜底。
//
// 权重全零说明调用方没表达任何意图，用默认策略；只要有任意一个非零，就按原样
// 执行（允许 0 = 关掉某条通道）。
func (p RetrievalPolicy) normalize() RetrievalPolicy {
	fallback := defaultRetrievalPolicy()
	if p.ExactWeight == 0 && p.KeywordWeight == 0 && p.VectorWeight == 0 {
		p.ExactWeight = fallback.ExactWeight
		p.KeywordWeight = fallback.KeywordWeight
		p.VectorWeight = fallback.VectorWeight
	}
	if p.RRFSmoothing <= 0 {
		p.RRFSmoothing = fallback.RRFSmoothing
	}
	if p.ExactCandidateLimit <= 0 {
		p.ExactCandidateLimit = fallback.ExactCandidateLimit
	}
	if p.KeywordCandidateLimit <= 0 {
		p.KeywordCandidateLimit = fallback.KeywordCandidateLimit
	}
	if p.VectorCandidateLimit <= 0 {
		p.VectorCandidateLimit = fallback.VectorCandidateLimit
	}
	return p
}

// ChannelState 是一条通道在一次检索里的参与情况。
type ChannelState struct {
	Name string
	// Candidates 是回填成功、且通过范围与可见性过滤之后的候选数。
	// 为 0 只说明没命中，不代表通道有问题。
	Candidates int
	// Err 非空表示这条通道降级了：没跑起来（超时、限流、集群不可达…）。
	Err string
}

// Report 是一次检索的通道台账。
//
// 它的用途是把「结果为什么这么少」变成能直接回答的问题：是某条通道挂了
// （Err 非空），还是通道都正常但确实没命中（Candidates 全 0），还是某条通道
// 压根没启用（被权重关掉，不出现在报告里）。只看最终结果条数，这三种情况长得
// 一模一样。
type Report struct {
	Channels []ChannelState
}

// Used 列出真正给出了候选的通道名。
func (r Report) Used() []string {
	names := make([]string, 0, len(r.Channels))
	for _, channel := range r.Channels {
		if channel.Candidates > 0 {
			names = append(names, channel.Name)
		}
	}
	return names
}

// Degraded 列出降级的通道名。
func (r Report) Degraded() []string {
	names := make([]string, 0, len(r.Channels))
	for _, channel := range r.Channels {
		if channel.Err != "" {
			names = append(names, channel.Name)
		}
	}
	return names
}

// filterDSLKey 是 Filter 在 retriever.Options.DSLInfo 里的键。
//
// 为什么走 DSLInfo：eino 的 retriever.Option 字段未导出，只能由它自己那几个
// 构造函数产生；DSLInfo 是官方留给后端自定义过滤表达式的通道。范围与可见性
// 是检索必须收窄的条件，塞不进这个通道就只能靠调用方自己记得再过滤一遍 ——
// 而「忘了过滤」的后果是跨数据集串味或越权。
const filterDSLKey = "filter"

// WithFilter 把数据集范围与可见性条件传进一次检索。
//
// 单独给一个构造函数，而不是让调用方自己拼 DSLInfo：键名或类型写错不会有任何
// 提示，而失败的表现正是「过滤没生效」。检索侧会在类型不符时直接报错。
func WithFilter(filter Filter) retriever.Option {
	return retriever.WithDSLInfo(map[string]any{filterDSLKey: filter})
}

// filterFromOptions 取出调用方传进来的 Filter。
//
// 类型不符时报错而不是回落成「不限」：回落等于把一次带过滤条件的检索悄悄放大成
// 全库检索，那是越权而不是降级。
func filterFromOptions(common *retriever.Options) (Filter, error) {
	raw, ok := common.DSLInfo[filterDSLKey]
	if !ok {
		return Filter{}, nil
	}
	filter, ok := raw.(Filter)
	if !ok {
		return Filter{}, fmt.Errorf("rag: DSLInfo[%q] 必须是 rag.Filter，收到 %T", filterDSLKey, raw)
	}
	return filter, nil
}

// HybridRetriever 实现 eino retriever.Retriever：三条通道加权 RRF 融合，可选重排。
type HybridRetriever struct {
	store    *Store
	embedder *Embedder
	reranker Reranker // 可为 nil
	logger   *slog.Logger

	policy    RetrievalPolicy
	rrfK      float64
	defTopK   int
	defThresh float64
}

type HybridConfig struct {
	Store    *Store
	Embedder *Embedder
	Reranker Reranker // 可选
	Logger   *slog.Logger

	TopK int // 默认 5

	// Retrieval 是三条通道的权重、融合平滑与候选上限。全零时用内置默认值。
	Retrieval RetrievalPolicy

	// ScoreThreshold 是**单通道原始分**的下限：余弦相似度、BM25 分、词元命中率。
	// 0 表示不过滤。
	//
	// 它刻意不作用在融合分上：融合分只是名次的函数（量级在 1/rrfSmoothing 上下），
	// 拿它跟「0.5」这种相似度量级的数字比，结果是**一条都留不下**，而症状与
	// 「召回坏了」完全一样。原始分虽然量纲各不相同，但各自都在「越高越相关」
	// 的自然区间里，做下限才有意义。判定口径是「任一通道的原始分高于它即保留」，
	// 所以调高它不会误伤另一条通道捞回来的结果。
	ScoreThreshold float64
}

// NewHybridRetriever 组装检索器。
//
// Embedder 允许为 nil：那种情况下向量通道自动关闭，只剩精确与关键词两条通道。
// 这不是「勉强能用」，而是一个合法部署 —— 召回的主力通道（型号、系列、品类）
// 本来就不依赖外部模型，没有 embedding 就不该连检索都开不起来。
func NewHybridRetriever(cfg HybridConfig) (*HybridRetriever, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("rag: store is required")
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	policy := cfg.Retrieval.normalize()
	if cfg.Embedder == nil {
		// 关掉而不是留着报错：留着会让每一次响应的 degraded 都带上 vector，
		// 那条信息就不再指示任何异常了（它本该表示「这次运行有个东西坏了」）。
		if policy.VectorWeight > 0 {
			cfg.Logger.Warn("没有可用的 embedding 客户端，向量通道已关闭（本次为纯关键词召回）")
		}
		policy.VectorWeight = 0
	}
	return &HybridRetriever{
		store:     cfg.Store,
		embedder:  cfg.Embedder,
		reranker:  cfg.Reranker,
		logger:    cfg.Logger,
		policy:    policy,
		rrfK:      float64(policy.RRFSmoothing),
		defTopK:   cfg.TopK,
		defThresh: cfg.ScoreThreshold,
	}, nil
}

// Retrieve 实现 eino retriever.Retriever。
//
// 注意：query embedding 必须与入库时使用同一个模型！
func (h *HybridRetriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	docs, _, err := h.RetrieveDetailed(ctx, query, opts...)
	return docs, err
}

// RetrieveDetailed 与 Retrieve 同义，额外返回通道台账。
//
// 需要「本次结果来自哪条通道、有没有通道降级」的调用方（HTTP 响应、排查日志）
// 用它；只需要结果的走 Retrieve。
//
// 通道的失败处理是不对称的，这是有意的：
//
//   - 精确与关键词通道只依赖 PostgreSQL / 检索索引，是型号类查询的主要命中面。
//   - 向量通道依赖 embedding 服务，抖动与限流都更常见，而且它补的是表述差异。
//
// 所以任意一条通道失败都只降级、不整体失败；**三条全失败**才返回错误。早先的
// 实现是「先 embedding，失败即返回错误」—— 那等于让外部模型的可用性决定
// 「能不能按型号搜到东西」，而这本来是不需要的。
func (h *HybridRetriever) RetrieveDetailed(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, Report, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, Report{}, fmt.Errorf("rag: empty query")
	}

	// [优化] 复用 eino 通用 Option（TopK / ScoreThreshold 可被调用方动态覆盖）
	common := &retriever.Options{}
	retriever.GetCommonOptions(common, opts...)

	topK := h.defTopK
	if common.TopK != nil {
		topK = *common.TopK
	}
	if topK <= 0 {
		return nil, Report{}, fmt.Errorf("rag: topK must be greater than zero")
	}
	thresh := h.defThresh
	if common.ScoreThreshold != nil {
		thresh = *common.ScoreThreshold
	}
	filter, err := filterFromOptions(common)
	if err != nil {
		return nil, Report{}, err
	}

	fused, report, err := h.runChannels(ctx, query, topK, filter)
	if err != nil {
		return nil, report, err
	}

	// 质量过滤。阈值判的是通道原始分（见 HybridConfig.ScoreThreshold），
	// 保留任意一条通道给够分的结果。
	out := make([]*schema.Document, 0, len(fused))
	for _, item := range fused {
		if strings.TrimSpace(item.doc.Content) == "" {
			continue
		}
		if thresh > 0 && item.raw < thresh {
			continue
		}
		out = append(out, item.doc)
	}

	// [优化] 精排
	if h.reranker != nil {
		out, err = h.reranker.Rerank(ctx, query, out)
		if err != nil {
			return nil, report, fmt.Errorf("rag: rerank: %w", err)
		}
	}

	if len(out) > topK {
		out = out[:topK]
	}
	return out, report, nil
}

// channelPlan 描述一条通道：叫什么、有多重、取多少候选、怎么执行。
type channelPlan struct {
	name   Channel
	weight float64
	limit  int
	run    func(ctx context.Context, limit int) ([]Hit, error)
}

// channelResult 是一条通道跑完之后交给融合的东西。
type channelResult struct {
	hits   []Hit
	weight float64
}

// runChannels 按优先级依次执行启用的通道，返回融合后的结果与台账。
//
// 顺序执行而不是并发：三条通道里两条打 PostgreSQL、一条打 Milvus，串起来跑
// 才能让「关键词先出结果、向量后补」这件事在时序上真的成立，排查时日志也
// 按优先级排列，不用对着交错的时间戳还原。
func (h *HybridRetriever) runChannels(ctx context.Context, query string, topK int, filter Filter) ([]fusedHit, Report, error) {
	plans := []channelPlan{
		{
			name:   ChannelExact,
			weight: h.policy.ExactWeight,
			limit:  h.policy.ExactCandidateLimit,
			run: func(ctx context.Context, limit int) ([]Hit, error) {
				return h.store.SearchByExact(ctx, query, limit, filter)
			},
		},
		{
			name:   ChannelKeyword,
			weight: h.policy.KeywordWeight,
			limit:  h.policy.KeywordCandidateLimit,
			run: func(ctx context.Context, limit int) ([]Hit, error) {
				return h.store.SearchByText(ctx, query, limit, filter)
			},
		},
		{
			name:   ChannelVector,
			weight: h.policy.VectorWeight,
			limit:  h.policy.VectorCandidateLimit,
			run: func(ctx context.Context, limit int) ([]Hit, error) {
				vecs, err := h.embedder.EmbedStrings(ctx, []string{query})
				if err != nil {
					return nil, fmt.Errorf("rag: embed query: %w", err)
				}
				if len(vecs) == 0 {
					return nil, errors.New("rag: embed query returned no vector")
				}
				return h.store.SearchByVector(ctx, vecs[0], limit, filter)
			},
		},
	}

	report := Report{Channels: make([]ChannelState, 0, len(plans))}
	results := make([]channelResult, 0, len(plans))
	failures := make([]string, 0, len(plans))
	succeeded := 0

	for _, plan := range plans {
		if plan.weight <= 0 {
			// 权重 0 = 这条通道被配置关掉了。它不进报告：那不是「降级」而是
			// 「没启用」，混在一起会让人去排查一个根本没开的通道。
			continue
		}
		limit := plan.limit
		if limit < topK {
			// 候选数不能小于最终条数，否则过滤之后必然凑不满。
			limit = topK
		}

		hits, err := plan.run(ctx, limit)
		state := ChannelState{Name: string(plan.name)}
		if err != nil {
			state.Err = err.Error()
			failures = append(failures, fmt.Sprintf("%s: %v", plan.name, err))
			h.logger.WarnContext(ctx, "检索通道降级，本次结果不含这条通道的贡献",
				slog.String("channel", string(plan.name)),
				slog.String("query", query),
				slog.String("error", err.Error()),
			)
		} else {
			succeeded++
			state.Candidates = len(hits)
			if len(hits) > 0 {
				results = append(results, channelResult{hits: hits, weight: plan.weight})
			}
		}
		report.Channels = append(report.Channels, state)
	}

	// 判定口径是「有没有一条通道成功」，不是「有没有结果」：通道都正常但确实
	// 没命中，返回空结果是正确答案，不是错误。反过来只要有通道跑成功，它的答案
	// （哪怕为空）就是可信的 —— 把空结果报成错误会让调用方把它当故障重试。
	if succeeded == 0 && len(failures) > 0 {
		return nil, report, fmt.Errorf("rag: 所有检索通道都失败: %s", strings.Join(failures, "; "))
	}
	return h.rrfFuse(results), report, nil
}

// fusedHit 是一条融合后的结果。
//
// 它同时带着两个分数，因为它们回答不同的问题：fused 决定顺序（由名次与通道权重
// 算出），raw 回答「这条结果到底有多像」（通道自己的相似度 / BM25 分），质量
// 过滤看后者。
type fusedHit struct {
	doc   *schema.Document
	fused float64
	raw   float64
}

// rrfFuse 做加权 RRF：score = Σ 通道权重 / (k + 名次)
//
// 分数只由名次与权重算出，所以三条通道的量纲差异（BM25 分、余弦相似度、词元
// 命中率）不影响融合结果 —— 这正是当初把「通道」和「通道的实现」分开时定下的
// 口径。原始分单独记在 raw 上透传，供质量过滤与诊断使用。
//
// 权重在这里参与而不是在通道内部：通道只负责「按自己的口径排好序」，谁更重要
// 是检索策略的事（配置的 retrieval 段），两者混在一起会出现「通道自己缩放分数
// 影响名次」这种说不清的效果。
func (h *HybridRetriever) rrfFuse(results []channelResult) []fusedHit {
	type acc struct {
		doc *schema.Document
		sum float64
		raw float64
	}
	byID := make(map[string]*acc)
	order := make([]string, 0)

	for _, result := range results {
		for rank, hit := range result.hits {
			id := hit.Doc.ID
			if id == "" {
				id = fmt.Sprintf("%s|%s", MetaString(hit.Doc, constant.MetaSource), hit.Doc.Content[:min(len(hit.Doc.Content), 64)])
			}
			a, ok := byID[id]
			if !ok {
				clone := *hit.Doc
				a = &acc{doc: &clone}
				byID[id] = a
				order = append(order, id)
			}
			a.sum += result.weight / (h.rrfK + float64(rank+1))
			if hit.Score > a.raw {
				a.raw = hit.Score
			}
		}
	}

	out := make([]fusedHit, 0, len(order))
	for _, id := range order {
		a := byID[id]
		a.doc.WithScore(a.sum)
		out = append(out, fusedHit{doc: a.doc, fused: a.sum, raw: a.raw})
	}
	// 按融合分数降序
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].fused > out[j-1].fused; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

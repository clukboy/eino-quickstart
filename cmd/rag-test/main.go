// Command rag-test 跑检索召回的离线评测。
//
// 它回答一个具体问题：库里已经索引好的内容，能不能被问出来。做法是拿一份
// 金标用例集（query + 期望命中的文档 source）去打真实检索链路，把命中折算成
// Recall@K / MRR / ACL 泄漏 / P95，再按 thresholds.yaml 决定退出码 —— 所以它
// 既能手动跑着看，也能直接卡在 CI 或发布流水线里阻断质量回退。
//
// 它与 cmd/ragserver 的区别是「谁问了算数」：ragserver 是人在终端里随手问，
// 看的是单次结果；本命令跑的是固定用例集，看的是指标随版本的变化。
//
// 链路：configs/config.yaml -> PostgreSQL + Milvus + Elasticsearch + embedding ->
// rag.Store（向量通道 + 词法通道）-> rag.HybridRetriever（RRF 融合）->
// internal/eval（指标）-> 报告 + 退出码。
//
// 词法通道有两条实现：配了 es.address 走 BM25，没配回落 PostgreSQL 子串匹配。
// 报告开头会写明这次跑的是哪一条 —— 同一份用例集在两条通道下的分数没有可比性。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/ent/documentchunk"
	"eino-quickstart/internal/eval"
	"eino-quickstart/internal/platform/config"
	"eino-quickstart/internal/platform/storage/entx"
	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag"
	"eino-quickstart/internal/rag/constant"
	"eino-quickstart/internal/rag/grouping"
	ragparser "eino-quickstart/internal/rag/parser"

	"github.com/cloudwego/eino-ext/components/embedding/openai"
	"github.com/cloudwego/eino/components/retriever"
)

const (
	defaultBusinessConfig = "./configs/config.yaml"
	defaultDataset        = "internal/eval/datasets/retrieval.jsonl"
	defaultThresholds     = "internal/eval/thresholds.yaml"

	// 默认报告落在 logs/ 下：那一整个目录已经在 .gitignore 里，
	// 评测产物不该混进 git status 让人误提交。
	defaultReportPath = "logs/eval-report.json"

	queryTimeout = 2 * time.Minute
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("%v", err)
	}
}

func run() error {
	var (
		configPath = flag.String("config", envOr("EINO_CONFIG", defaultBusinessConfig), "业务配置路径")
		thresholds = flag.String("thresholds", defaultThresholds, "门禁配置路径，置空则只评测不卡门禁")
		topK       = flag.Int("topk", 0, "默认 topK，0 表示取 knowledge.defaultTopK")
		reportPath = flag.String("out", defaultReportPath, "报告 JSON 落盘路径，置空则不落盘")
		verbose    = flag.Bool("v", false, "打印每条用例的明细")
		// 粒度按知识库类型配（configs/config.yaml 的 knowledge.recallGrouping）。
		// 语料混了好几类库时，逐个类型跑一轮再比，别指望一份数字覆盖两类库。
		datasetType = flag.String("dataset-type", "",
			"评测语料的数据集类型，用来查该库的召回归并粒度；留空取 recallGrouping 的 default")
		datasets stringList
	)
	flag.Var(&datasets, "dataset", "金标用例集路径（JSONL），可重复指定，默认 "+defaultDataset)
	flag.Parse()

	if len(datasets) == 0 {
		datasets = append(datasets, defaultDataset)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("加载配置: %w", err)
	}

	// 粒度策略在开跑前解析：粒度名写错（"documents"）应当在这里就报出来，
	// 而不是静默回落成默认粒度 —— 后者会让整轮评测的数字换一个单位，
	// 却长得和「召回变差」一模一样。
	groupingPolicy, err := grouping.NewPolicy(cfg.Knowledge.RecallGrouping)
	if err != nil {
		return fmt.Errorf("解析 knowledge.recallGrouping: %w", err)
	}
	recallGrouping := groupingPolicy.For(*datasetType)

	ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
	defer cancel()

	entClient, err := entx.Open(ctx, cfg.Storage)
	if err != nil {
		return fmt.Errorf("连接 PostgreSQL: %w", err)
	}
	defer func() { _ = entClient.Close() }()

	// 用例先装载再连外部服务：用例集写错了（路径、id 重复）应该在
	// 连 Milvus 之前就报出来，省掉一轮无意义的连接与等待。
	cases, err := eval.LoadCases(datasets...)
	if err != nil {
		return err
	}

	clients, err := newSearchClients(ctx, entClient, cfg)
	if err != nil {
		return err
	}
	defer clients.close(ctx)

	// 预检：把「跑了也全是 0」的情况在开跑前挑明。静默的全 0 结果最容易被
	// 误读成「召回坏了」，而真实原因往往是索引还没跑、或者用例里的 source
	// 写错了。两者都在这里拦掉。
	if err := preflight(ctx, entClient, clients, cases); err != nil {
		return err
	}

	effectiveTopK := *topK
	if effectiveTopK <= 0 {
		effectiveTopK = cfg.Knowledge.DefaultTopK
	}
	if effectiveTopK <= 0 {
		effectiveTopK = 5
	}

	runner := eval.Runner{
		Searcher:    retrieverSearcher{retriever: clients.retriever},
		TopK:        effectiveTopK,
		Granularity: recallGrouping,
		Logger:      slog.Default(),
	}
	report := runner.Run(ctx, cases)

	// 通道与粒度必须在报告之前打出来：同一份用例集换一条词法通道、换一种归并
	// 粒度，同一个数字的含义都会变。报告一旦离开这个上下文就没人知道当时跑的是
	// 哪一套 —— 「召回 3 条」是 3 篇文档还是 3 个分块，差得很远。
	fmt.Printf("关键词通道: %s\n", clients.keywordChannel())
	fmt.Printf("召回归并粒度: %s\n\n", groupingPolicy.Describe(*datasetType))

	if *verbose {
		if err := renderDetail(os.Stdout, report); err != nil {
			return fmt.Errorf("输出用例明细: %w", err)
		}
	}
	if err := eval.Render(os.Stdout, report); err != nil {
		return fmt.Errorf("输出报告: %w", err)
	}

	if *reportPath != "" {
		if err := os.MkdirAll(filepath.Dir(*reportPath), 0o755); err != nil {
			return fmt.Errorf("创建报告目录: %w", err)
		}
		if err := eval.WriteJSON(*reportPath, report); err != nil {
			return err
		}
		fmt.Printf("\n报告已写入 %s\n", *reportPath)
	}

	// 门禁判定放在最后：无论过不过，人已经先看到完整报告了。
	if *thresholds == "" {
		fmt.Println("\n已跳过门槛检查（-thresholds 为空）")
		return nil
	}
	limits, err := eval.LoadThresholds(*thresholds)
	if err != nil {
		return err
	}
	violations := limits.Check(report.Summary)
	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("未通过发布门槛：\n  - %s", strings.Join(violations, "\n  - "))
}

// searchClients 是本命令需要的外部依赖。
type searchClients struct {
	store     *rag.Store
	retriever retriever.Retriever
	// searchIndex 为 nil 表示没配 ES，关键词通道走 PostgreSQL 子串匹配。
	// 预检与报告都要知道这件事：同一份用例集在两条通道下的分数不可直接比较。
	searchIndex *es.Client
}

func (c *searchClients) close(ctx context.Context) {
	if c.store == nil || c.store.MilvusStore == nil {
		return
	}
	if err := c.store.MilvusStore.Close(ctx); err != nil {
		slog.Default().Warn("关闭 Milvus 连接失败", slog.String("error", err.Error()))
	}
}

// keywordChannel 是报告里显示的通道名。
func (c *searchClients) keywordChannel() string {
	if c.searchIndex == nil {
		return "PostgreSQL 子串匹配（未配置 ES）"
	}
	return "Elasticsearch BM25（索引 " + c.searchIndex.Index() + "）"
}

// newSearchClients 组装检索链路。
//
// ES 是可选的：配了就注入，检索的词法通道走 BM25；没配就注入 nil，回落
// PostgreSQL 子串匹配。两条通道都能跑评测 —— 但报告里会把用的是哪条写清楚，
// 因为同一份用例集在两条通道下的分数没有可比性。
func newSearchClients(ctx context.Context, entClient *ent.Client, cfg *config.Config) (*searchClients, error) {
	apiKey := strings.TrimSpace(os.Getenv(cfg.Embedding.APIKeyEnv))
	if apiKey == "" {
		return nil, fmt.Errorf("环境变量 %s 未设置（embedding API Key）", cfg.Embedding.APIKeyEnv)
	}
	embedder, err := rag.NewEmbedder(ctx, &openai.EmbeddingConfig{
		APIKey:     apiKey,
		BaseURL:    cfg.Embedding.BaseURL,
		Model:      cfg.Embedding.Model,
		Dimensions: &cfg.Embedding.Dimensions,
	}, rag.WithMaxTextsPerRequest(cfg.Embedding.BatchSize))
	if err != nil {
		return nil, fmt.Errorf("初始化 embedding 客户端: %w", err)
	}

	searchIndex, err := es.New(&cfg.ES)
	if err != nil {
		return nil, fmt.Errorf("初始化检索索引客户端: %w", err)
	}

	store, err := rag.NewStore(ctx, entClient, cfg, searchIndex.Searcher())
	if err != nil {
		return nil, fmt.Errorf("初始化存储: %w", err)
	}

	// 查询 embedding 必须与入库用同一个模型，否则向量空间对不上，
	// 召回会退化成随机 —— 这一点由两边读同一段 embedding 配置来保证。
	hybrid, err := rag.NewHybridRetriever(rag.HybridConfig{
		Store:     store,
		Embedder:  embedder,
		TopK:      cfg.Knowledge.DefaultTopK,
		Retrieval: rag.PolicyFromConfig(cfg.Retrieval),
	})
	if err != nil {
		return nil, fmt.Errorf("初始化检索器: %w", err)
	}

	return &searchClients{store: store, retriever: hybrid, searchIndex: searchIndex}, nil
}

// preflight 检查「这次评测有没有意义」。
//
// 四道检查，每一道都对应一种会把结果读错的情形：
//
//  1. 向量集合不可用 —— 检索必然返回空。此时报告里的 0 不是召回质量，是环境问题。
//  2. 已索引分块为 0 —— 文档可能还在 indexing、索引任务失败、或压根没上传过。
//  3. 用例引用的 source 不在库里 —— 这类用例永远不可能通过，而且看上去和
//     「召回不准」一模一样。写错 source 是评测集最常见的事故。
//  4. 配了 ES 但索引是空的 —— 关键词通道必然全空，向量通道单打独斗。这会让
//     「配置忘了生效」看起来像「BM25 没用」，是引入 ES 之后最容易误判的一种。
func preflight(
	ctx context.Context,
	entClient *ent.Client,
	clients *searchClients,
	cases []eval.Case,
) error {
	slog.Default().Info("关键词通道", slog.String("channel", clients.keywordChannel()))

	if err := clients.store.MilvusStore.Ready(ctx); err != nil {
		return fmt.Errorf(
			"向量集合不可用: %w\n"+
				"提示：集合是在 cmd/worker 启动时创建的。先按 docs/rag-testing.md 上传文档并运行 worker",
			err,
		)
	}

	indexed, err := entClient.DocumentChunk.Query().
		Where(documentchunk.VectorStatusEQ(documentchunk.VectorStatusIndexed)).
		Count(ctx)
	if err != nil {
		return fmt.Errorf("统计已索引分块: %w", err)
	}
	if indexed == 0 {
		return errors.New(
			"库里没有任何已索引的分块，评测结果必然全为 0\n" +
				"提示：确认 cmd/worker 在跑，且文档状态已是 ready（GET /api/v1/dataset/:id/documents）",
		)
	}

	if err := preflightSearchIndex(ctx, clients, indexed); err != nil {
		return err
	}

	sources, statuses, err := corpusSnapshot(ctx, entClient)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return errors.New("documents 表为空，没有可检索的语料")
	}

	unknown := unknownSources(cases, sources)
	if len(unknown) > 0 {
		report := strings.Join(unknown, "\n  - ")
		if len(unknown) == len(distinctSources(cases)) {
			return fmt.Errorf(
				"用例引用的 source 没有一个存在于库中，用例集与语料不匹配：\n  - %s\n"+
					"提示：source 是相对 knowledge.root 的路径，可用下面这条 SQL 看实际值\n"+
					"  SELECT source FROM documents ORDER BY id",
				report,
			)
		}
		slog.Default().Warn("部分用例引用的 source 不在库中，这些用例必定失败",
			slog.String("sources", report),
		)
	}

	slog.Default().Info("评测前置检查通过",
		slog.Int("indexed_chunks", indexed),
		slog.Int("documents", len(sources)),
		slog.Any("document_status", statuses),
		slog.Int("cases", len(cases)),
	)
	return nil
}

// preflightSearchIndex 检查关键词索引能不能真的给出结果。
//
// 三件事：配没配 ES、索引里有没有文档、里面的文档是不是按当前映射写的。
// 第三件容易被忽略 —— 补写 mapping 只让索引有了字段，**已经写进去的文档不会
// 因此长出这些值**，而这两件事在评测报告里长得一模一样（都是「按型号搜不到」）。
//
// 这里只读不写：建索引是 cmd/worker 的启动步骤，评测工具往集群里建索引会让
// 「索引形态由谁定义」出现第二个说法。
func preflightSearchIndex(ctx context.Context, clients *searchClients, indexedChunks int) error {
	if clients.searchIndex == nil {
		// 没配 ES 不是错误：词法通道回落 PostgreSQL 子串匹配，评测照样有意义。
		slog.Default().Warn(
			"未配置检索索引，关键词通道走 PostgreSQL 子串匹配（没有词频与 IDF 权重）",
			slog.String("hint", "配好 configs/config.yaml 的 es.address 可评测真实的 BM25 召回"),
		)
		return nil
	}

	docs, err := clients.searchIndex.DocCount(ctx)
	if err != nil {
		return fmt.Errorf(
			"检索索引 %q 不可读: %w\n"+
				"提示：索引是在 cmd/worker 启动时创建的。先跑 worker，再上传文档让它索引",
			clients.searchIndex.Index(), err,
		)
	}
	if docs == 0 {
		return fmt.Errorf(
			"检索索引 %q 里一条文档都没有，关键词通道必然返回空结果（评测看到的会是「BM25 没用」）\n"+
				"提示：分块是在 cmd/worker 索引文档时写入 ES 的。确认 worker 的 es 段配置与本次评测一致，"+
				"并且文档已经索引过一遍（可对已有文档逐个触发 reindex）",
			clients.searchIndex.Index(),
		)
	}
	// 数量不一致不一定是错的（重新切块、删除文档都会让两边短暂错开），
	// 但差距很大时值得一看：那通常意味着有一批文档的 ES 写入失败过。
	if int64(indexedChunks) != docs {
		slog.Default().Warn("检索索引文档数与已索引分块数不一致",
			slog.Int("indexed_chunks", indexedChunks),
			slog.Int64("search_index_docs", docs),
			slog.String("hint", "差异过大多半是有文档的 ES 写入失败过；对受影响文档触发 reindex 即可补齐"),
		)
	}

	// 每份文档都盖着写入时的映射形态指纹；指纹与当前映射不一致的，就是还没
	// 按现在的检索面回填过的文档 —— 按型号、系列、品类都搜不到它们。这会让
	// 升级后的第一次评测得出「加了 ES 反而更差」的结论，而真相是一批文档
	// 还没回填。
	stale, err := clients.searchIndex.StaleDocs(ctx)
	if err != nil {
		// 陈旧计数失败不该拦住评测：它是提示，不是判据。
		slog.Default().Warn("统计形态与当前映射不一致的文档失败", slog.String("error", err.Error()))
		return nil
	}
	if stale > 0 {
		slog.Default().Warn("检索索引里有一批文档的形态与当前映射不一致（按映射声明的字段搜不到）",
			slog.Int64("stale_docs", stale),
			slog.Int64("total_docs", docs),
			slog.String("hint", "这些文档是在映射变更之前写入的，触发 reindex 即可回填："+
				"POST /api/v1/dataset/:id/documents/:docId/reindex"),
		)
	}
	return nil
}

// corpusSnapshot 取回库里所有文档的 source 与状态分布。
func corpusSnapshot(ctx context.Context, entClient *ent.Client) (map[string]struct{}, map[string]int, error) {
	docs, err := entClient.Document.Query().
		Select(document.FieldSource, document.FieldStatus).
		All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("读取 documents: %w", err)
	}

	sources := make(map[string]struct{}, len(docs))
	statuses := make(map[string]int, 4)
	for _, doc := range docs {
		source := strings.TrimSpace(doc.Source)
		if source != "" {
			sources[source] = struct{}{}
		}
		statuses[string(doc.Status)]++
	}
	return sources, statuses, nil
}

// unknownSources 找出用例里引用了、但库里不存在的 source。
func unknownSources(cases []eval.Case, sources map[string]struct{}) []string {
	seen := make(map[string]struct{}, 8)
	missing := make([]string, 0, 8)
	for _, item := range cases {
		for _, source := range append(append([]string{}, item.Expected...), item.Forbidden...) {
			if _, exists := sources[source]; exists {
				continue
			}
			if _, reported := seen[source]; reported {
				continue
			}
			seen[source] = struct{}{}
			missing = append(missing, source)
		}
	}
	return missing
}

func distinctSources(cases []eval.Case) []string {
	seen := make(map[string]struct{}, 8)
	out := make([]string, 0, 8)
	for _, item := range cases {
		for _, source := range append(append([]string{}, item.Expected...), item.Forbidden...) {
			if _, exists := seen[source]; exists {
				continue
			}
			seen[source] = struct{}{}
			out = append(out, source)
		}
	}
	return out
}

// retrieverSearcher 把 eino retriever 适配成评测用的 Searcher。
//
// 适配只做一件事：把检索结果里的 source 提取出来。评测按 source 判定命中，
// 而 source 有两个可能的键 —— Store 重建元数据时写的是 constant.MetaSource，
// Pipeline 的 loader 写的是 parser.MetaKeySource（"_source"）。两个都读，
// 免得检索实现换一种元数据写法之后评测静默地全部判失配。
type retrieverSearcher struct {
	retriever retriever.Retriever
}

func (s retrieverSearcher) Search(ctx context.Context, query string, topK int) ([]eval.Hit, error) {
	docs, err := s.retriever.Retrieve(ctx, query, retriever.WithTopK(topK))
	if err != nil {
		return nil, err
	}
	hits := make([]eval.Hit, 0, len(docs))
	for _, doc := range docs {
		if doc == nil {
			continue
		}
		chunkID, _ := strconv.ParseInt(doc.ID, 10, 64)
		hits = append(hits, eval.Hit{
			ChunkID:     chunkID,
			Source:      firstNonEmpty(rag.MetaString(doc, constant.MetaSource), rag.MetaString(doc, ragparser.MetaKeySource)),
			Title:       rag.MetaString(doc, constant.MetaTitle),
			HeadingPath: rag.MetaString(doc, constant.MetaHeadingPath),
			Score:       doc.Score(),
			Content:     doc.Content,
		})
	}
	return hits, nil
}

// renderDetail 打印每条用例的召回明细，用来人肉判断"差多少"。
//
// 明细按**归并粒度**逐条列出 —— 粒度由知识库类型决定（-dataset-type 配合
// configs/config.yaml 的 knowledge.recallGrouping）：产品型录一篇文档就是一个
// 产品，列出一条文档；普通文档库一个分块就是一条内容，逐个分块列出。把同一篇
// 文档的 8 个命中块摊成 8 行，等于让人自己再做一遍归并；反过来把 200 个分块压成
// 1 行，人就完全看不出内容被哪一段吃掉了。
//
// 每行带得分、命中块数与标题路径：得分用来看「差多远」（0.31 与 0.87 是两种
// 不同的问题），块数用来看「是不是同一篇文档占掉了好几个名次」，标题路径
// 用来看「是产品的哪个字段 / 文档的哪一节命中的」。
func renderDetail(out io.Writer, report eval.Report) error {
	granularity := report.Granularity
	if granularity == "" {
		granularity = grouping.Default
	}
	fmt.Fprintf(out, "\n用例明细（召回结果逐条按%s列出）\n", granularity.Label())
	fmt.Fprintln(out, strings.Repeat("─", 68))
	for _, result := range report.Cases {
		mark := "✓"
		if !result.Passed {
			mark = "✗"
		}
		fmt.Fprintf(out, "  %s %-24s recall=%.2f rank=%d 结果 %d 条（命中 %d 块）%dms  query=%q\n",
			mark, result.ID, result.Recall, result.FirstHitRank,
			len(result.Results), result.Chunks, result.DurationMS, result.Query)
		if err := renderResults(out, result.Results); err != nil {
			return err
		}
	}
	fmt.Fprintln(out)
	return nil
}

// renderResults 按名次列出召回到的每一条结果。
func renderResults(out io.Writer, results []eval.ResultHit) error {
	if len(results) == 0 {
		fmt.Fprintln(out, "        （无结果）")
		return nil
	}
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, result := range results {
		fmt.Fprintf(writer, "        %d. %s\t%.3f\t%d 块\t%s\n",
			result.Rank, result.Source, result.Score, result.Chunks,
			truncateRunes(result.HeadingPath, 36))
	}
	return writer.Flush()
}

// truncateRunes 按字符（不是字节）截断，避免把中文切成半个字。
func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// stringList 让 -dataset 可以重复出现。
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("dataset 路径不能为空")
	}
	*l = append(*l, value)
	return nil
}

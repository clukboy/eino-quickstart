package eval

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"eino-quickstart/internal/rag/grouping"
)

// groupResults 按配置的粒度把分块级命中整理成结果明细。
//
// 归并粒度由知识库类型决定（见 grouping.Policy），不在这里假设：产品型录一篇
// 文档就是一个产品，归并成文档才对；普通文档库一篇长文切成几百块，归并成一条
// 等于什么都没返回。写死成其中一边，另一类库的评测数字就是错的 —— 而且错得
// 像质量问题（条数忽多忽少），不像粒度问题。
func groupResults(hits []Hit, granularity grouping.Granularity) []ResultHit {
	if granularity == grouping.Chunk {
		return chunkResults(hits)
	}
	return documentResults(hits)
}

// chunkResults 原样返回每个分块，一条一块。
func chunkResults(hits []Hit) []ResultHit {
	results := make([]ResultHit, 0, len(hits))
	for index, hit := range hits {
		results = append(results, ResultHit{
			Rank:        index + 1,
			Source:      strings.TrimSpace(hit.Source),
			Title:       hit.Title,
			ChunkID:     hit.ChunkID,
			HeadingPath: hit.HeadingPath,
			Score:       hit.Score,
			Chunks:      1,
			Content:     hit.Content,
		})
	}
	return results
}

// documentResults 把同一篇文档的命中块并成一条。
//
// 归并键是 source（去空白后比较，检索实现可能带空格）。名次按**首次出现次序**，
// 代表块取**得分最高**的那一块：同一篇文档的多个命中块里，分高的最贴近查询，
// 它的正文与标题路径最能解释「为什么命中」。并列时保留先出现的，保证同样的输入
// 每次归纳出同样的结果 —— 报告要能两次 run 直接 diff。
//
// 命中块数一路带着：一篇文档占掉 TopK 里 6 个位置是「切块过碎、正在挤掉别的内容」
// 的信号，只看去重后的条数看不到它。
func documentResults(hits []Hit) []ResultHit {
	results := make([]ResultHit, 0, len(hits))
	position := make(map[string]int, len(hits))

	for _, hit := range hits {
		source := strings.TrimSpace(hit.Source)
		if at, seen := position[source]; seen {
			result := &results[at]
			result.Chunks++
			if hit.Score > result.Score {
				result.Score = hit.Score
				result.Content = hit.Content
				result.HeadingPath = hit.HeadingPath
				result.ChunkID = hit.ChunkID
				// 标题只在代表块确实带了的时候才覆盖：某些检索实现只在首块
				// 写标题，用空串覆盖会让这条结果凭空少掉身份信息。
				if hit.Title != "" {
					result.Title = hit.Title
				}
			}
			continue
		}

		position[source] = len(results)
		results = append(results, ResultHit{
			Rank:        len(results) + 1,
			Source:      source,
			Title:       hit.Title,
			ChunkID:     hit.ChunkID,
			HeadingPath: hit.HeadingPath,
			Score:       hit.Score,
			Chunks:      1,
			Content:     hit.Content,
		})
	}
	return results
}

// evaluateCase 把一次召回的原始结果折算成用例结果。
//
// 判定口径都是刻意的，改之前先看清代价：
//
//  1. **命中按 document source 去重**。同一篇文档会切成很多块，评测问的是
//     「这篇文档有没有被找到」，不是「找到几段」。不做去重的话，一篇文档命中
//     8 个块就会把那 8 个名次全算成有效召回，MRR 会被切块粒度污染 —— 切得更
//     碎反而指标更好看，这正是评测最该避免的激励。
//
//     注意这一条**与归并粒度无关**：即使按 chunk 粒度输出结果，Recall / MRR
//     仍然按文档去重。质量指标一旦跟着切块粒度走，调小 chunkSize 就能把分数
//     刷上去。
//
//  2. **越权一票否决**。命中 Forbidden 直接判失败，不让高 Recall 抵消：
//     泄漏是安全事件，不是质量分项。
//
//  3. **无答案用例（Expected 为空）的 Recall 视作 1**。它没有「该召回到什么」
//     可谈，只有「不该召回什么」（由 Forbidden 表达）。给它记 0 会让均值
//     随这类用例的条数漂移。
//
// 明细（Results）按配置粒度整理，MinResults 也跟着那个粒度判 —— 它量的是
// 「调用方拿到几条」，不是质量分项。
func evaluateCase(c Case, hits []Hit, latencyMS int64, granularity grouping.Granularity) CaseResult {
	results := groupResults(hits, granularity)

	result := CaseResult{
		ID:            c.ID,
		Scene:         c.Scene,
		Query:         c.Query,
		Note:          c.Note,
		ExpectedCount: len(c.Expected) + len(c.ExpectedKeywords),
		Hits:          len(results),
		Chunks:        len(hits),
		DurationMS:    latencyMS,
		Results:       results,
	}

	// 判定用的名次表恒按**文档**建（按首次出现次序，1 起）。名次而不是分数
	// 决定一切：两个通道的量纲完全不同，分数不可比，名次可比。
	rankOf := make(map[string]int, len(results))
	retrieved := make([]string, 0, len(results))
	for _, hit := range hits {
		source := strings.TrimSpace(hit.Source)
		if _, seen := rankOf[source]; seen {
			continue
		}
		rankOf[source] = len(retrieved) + 1
		retrieved = append(retrieved, source)
	}

	// 越权命中
	for _, forbidden := range c.Forbidden {
		if _, hit := rankOf[forbidden]; hit {
			result.Leaked = append(result.Leaked, forbidden)
		}
	}

	// 命中与漏召。两种口径都并进同一个 recall 分母：它们表达的是同一件事
	// （该被找到的文档有没有被找到），分开算会得出两个都看不出问题的指标。
	matched := 0
	recordHit := func(rank int) {
		matched++
		if result.FirstHitRank == 0 || rank < result.FirstHitRank {
			result.FirstHitRank = rank
		}
	}
	for _, want := range c.Expected {
		rank, hit := rankOf[want]
		if !hit {
			result.Missing = append(result.Missing, want)
			continue
		}
		recordHit(rank)
	}
	for _, keyword := range c.ExpectedKeywords {
		rank, hit := firstKeywordRank(retrieved, keyword)
		if !hit {
			result.MissingKeywords = append(result.MissingKeywords, keyword)
			continue
		}
		recordHit(rank)
	}

	if result.ExpectedCount > 0 {
		result.Recall = float64(matched) / float64(result.ExpectedCount)
	} else {
		result.Recall = 1
	}
	if result.FirstHitRank > 0 {
		result.ReciprocalRank = 1 / float64(result.FirstHitRank)
	}

	switch {
	case len(result.Leaked) > 0:
		result.Error = "forbidden source returned: " + strings.Join(result.Leaked, ", ")
	case result.ExpectedCount > 0 && matched < result.ExpectedCount:
		result.Error = "expected source missing: " + strings.Join(
			append(append([]string{}, result.Missing...), result.MissingKeywords...), ", ")
	case c.MinResults > 0 && len(results) < c.MinResults:
		// 报条数时把粒度写进去：hits 的「条」在两种粒度下不是同一个东西,
		// 提示信息里不带单位，看到的人会按自己以为的粒度去理解。
		result.Error = fmt.Sprintf("got %d %s results, expected at least %d",
			len(results), granularity, c.MinResults)
	default:
		result.Passed = true
	}
	return result
}

// firstKeywordRank 找出第一个 source 包含该关键词的名次（1 起）。
//
// 关键词口径是为托管上传的正文准备的：ContentStore.Create 用「slug + 纳秒时间戳」
// 命名，路径每次上传都会变，精确 source 写进用例集活不过一次重传。
// 匹配大小写不敏感，因为同一份文档在 Linux 与 macOS 上落盘的大小写可能不同。
func firstKeywordRank(retrieved []string, keyword string) (int, bool) {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return 0, false
	}
	for index, source := range retrieved {
		if strings.Contains(strings.ToLower(source), keyword) {
			return index + 1, true
		}
	}
	return 0, false
}

// summarize 汇总一轮评测。
//
// 三个比率的分母刻意不同，别统一：
//   - PassRate 的分母是全部用例（无答案与越权用例也参与，它们也要通过）
//   - Recall@K / MRR / HitRate@1 的分母只含「有期望」的用例
//
// 混用会让指标随用例集的场景配比漂移：多塞几条无答案用例，Recall 就"上升"了。
func summarize(results []CaseResult) Summary {
	summary := Summary{Cases: len(results)}

	recalls := make([]float64, 0, len(results))
	latencies := make([]int64, 0, len(results))
	var reciprocalSum float64
	var recallable, hitAt1 int

	for _, result := range results {
		latencies = append(latencies, result.DurationMS)
		if result.Passed {
			summary.Passed++
		} else {
			summary.Failed++
		}
		summary.ACLLeakCount += len(result.Leaked)
		if result.Hits == 0 {
			summary.EmptyResultCases++
		}

		if result.ExpectedCount == 0 {
			continue
		}
		recallable++
		recalls = append(recalls, result.Recall)
		reciprocalSum += result.ReciprocalRank
		if result.FirstHitRank == 1 {
			hitAt1++
		}
	}

	if summary.Cases > 0 {
		summary.PassRate = float64(summary.Passed) / float64(summary.Cases)
	}
	if recallable > 0 {
		summary.RecallAtK = mean(recalls)
		summary.MRR = reciprocalSum / float64(recallable)
		summary.HitRateAt1 = float64(hitAt1) / float64(recallable)
	}
	summary.P50LatencyMS = Percentile(latencies, 50)
	summary.P95LatencyMS = Percentile(latencies, 95)

	return summary
}

// sceneStats 按场景聚合，用来定位退化发生在哪一类查询上。
func sceneStats(results []CaseResult) []SceneStat {
	order := make([]string, 0)
	grouped := make(map[string][]CaseResult)
	for _, result := range results {
		scene := result.Scene
		if scene == "" {
			scene = "未分类"
		}
		if _, exists := grouped[scene]; !exists {
			order = append(order, scene)
		}
		grouped[scene] = append(grouped[scene], result)
	}

	stats := make([]SceneStat, 0, len(order))
	for _, scene := range order {
		group := grouped[scene]
		stat := SceneStat{Scene: scene, Cases: len(group)}
		var reciprocalSum float64
		var recallable int
		for _, result := range group {
			if result.Passed {
				stat.Passed++
			}
			if result.ExpectedCount == 0 {
				continue
			}
			recallable++
			reciprocalSum += result.ReciprocalRank
			stat.RecallAtK += result.Recall
		}
		if stat.Cases > 0 {
			stat.PassRate = float64(stat.Passed) / float64(stat.Cases)
		}
		if recallable > 0 {
			stat.RecallAtK /= float64(recallable)
			stat.MRR = reciprocalSum / float64(recallable)
		}
		stats = append(stats, stat)
	}
	return stats
}

// Percentile 用最近秩法（nearest-rank）算分位数：N 个样本的 p 分位是排序后
// 第 ceil(p/100*N) 个。
//
// 样本只有几十条时它比线性插值更好解释：报告里的 P95 一定对应某个真实用例的
// 耗时，而不是两个样本之间拟合出来的数。插值法在小样本下会低估长尾。
func Percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

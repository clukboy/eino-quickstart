package grouping

// 取数预算：把「要 N 条结果」翻译成「向检索侧要多少分块」。
//
// 为什么必须翻译：TopK 的单位是**结果**（条），而检索侧的单位是**分块**（见
// Granularity 的说明）。document 粒度下直接把 TopK 当分块数要，等于假设
// 「一篇文档只命中一块」—— 而一篇文档被切成几块由它的长度与 chunkSize 决定，
// 完全不由调用方控制。产品型录里一篇文档常切成四五块，实际表现就是
//
//	请求 top_k=20 → 拿回 20 个分块 → 归并成 4 篇文档
//
// 而这个数字还会随着调小 chunkSize 自动变大，看起来像召回质量在波动，实际是
// 单位没对齐。chunk 粒度下不存在这个问题（要 N 块就是要 N 条），所以预算是
// **按粒度**算的。
//
// 预算按需放大，而不是一次拍一个大倍数：兑换率（每篇文档命中几块）事先不知道，
// 常见的 2~4 块与病态的 50 块差一个量级，一次拍死了要么不够、要么白拉几十倍。
// 先按小倍数试，不够再翻倍，直到取满目标或检索侧明确见底。

const (
	// initialFetchFactor 是 document 粒度下第一次取块的放大倍数。
	//
	// 取 4 是「常见切块密度」与「不要一次拉太多」之间的折中：一篇文档被切成
	// 2~4 块时一轮就能取满，而它最多多花三倍候选，不是三倍延迟 —— 三条通道
	// 的候选上限本来就是并发一次取回的。
	initialFetchFactor = 4

	// maxChunkFetch 是单次召回的分块预算上限。
	//
	// 它挡的是病态情况：库里每篇文档被切成几十块，而调用方又要 200 篇 ——
	// 预算会一路翻到几千，而每一次翻倍都是一次完整的三通道检索。封顶之后
	// 轮数是常数（对数级），代价可算，同时也给「切块太碎」这件事留了个
	// 可观测的边界：到了上限还取不满，报告里的 matched_chunks 会说明。
	maxChunkFetch = 512
)

// FetchPlan 是一次召回的取数预算：想要几条、这一轮该要多少分块。
//
// 零值可用：want 为 0 表示没有目标，预算也就是 0，交给检索实现用它的默认值。
type FetchPlan struct {
	want   int
	budget int
}

// NewFetchPlan 为「取满 want 条结果」建一个预算计划。
//
// chunk 粒度下不放大：那一条结果就是一个分块，要 N 条就是要 N 块，放大只会
// 让检索侧白跑一轮候选。want <= 0 同样不放大 —— 没有目标就谈不上取满。
func NewFetchPlan(granularity Granularity, want int) FetchPlan {
	if want <= 0 {
		return FetchPlan{}
	}
	if granularity == Chunk {
		return FetchPlan{want: want, budget: want}
	}
	budget := want * initialFetchFactor
	if budget > maxChunkFetch {
		budget = maxChunkFetch
	}
	// 预算不能小于目标：调大 want 反而不放大是自相矛盾的，也会让「取满」
	// 这件事在一开始就不可能。
	if budget < want {
		budget = want
	}
	return FetchPlan{want: want, budget: budget}
}

// Want 是目标的条数（document 粒度下是篇数）。
func (p FetchPlan) Want() int { return p.want }

// Budget 是这一轮该向检索侧要的分块数。0 表示由检索实现决定。
func (p FetchPlan) Budget() int {
	if p.budget > 0 {
		return p.budget
	}
	return p.want
}

// Settled 报告这一轮之后该收手了。
//
// documents 是归并之后的条数，chunks 是这一轮实际拿到的分块数。三种收手理由
// 刻意合成一个判断，因为对调用方来说它们的结果一样（不再重试），但报告里要
// 分得清：
//
//	documents >= want          取满了 —— 正常出口
//	chunks < Budget            检索侧见底 —— 池子里就这么多，再要也没有
//	Budget >= maxChunkFetch    预算到顶 —— 再翻也只会更慢，不会更多
//
// 「chunks < budget」读作见底而不是「预算没用完」：检索实现只会给到候选池的
// 边界，给不满就说明那边已经到底了。少了这一条，库里只有 3 篇能匹配时，循环
// 会一路翻倍到上限才停 —— 每次都在问一个已知没有更多内容的池子。
func (p FetchPlan) Settled(documents, chunks int) bool {
	if p.want <= 0 || p.budget <= 0 {
		return true
	}
	if documents >= p.want {
		return true
	}
	if chunks < p.Budget() {
		return true
	}
	return p.budget >= maxChunkFetch
}

// Widen 把预算翻倍，并报告还能不能继续加码。
//
// 返回 false 表示已经到顶。调用方拿到 false 时应当直接收下当前结果，而不是
// 再用同样的预算重试一次 —— 那是同一份候选拉第二遍。
func (p FetchPlan) Widen() (FetchPlan, bool) {
	next := p.budget * 2
	if next > maxChunkFetch {
		next = maxChunkFetch
	}
	if next <= p.budget {
		return p, false
	}
	return FetchPlan{want: p.want, budget: next}, true
}

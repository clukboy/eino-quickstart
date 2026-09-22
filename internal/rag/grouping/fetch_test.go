package grouping

import "testing"

// chunk 粒度下预算与条数一一对应：那一条结果就是一个分块，放大只会让检索侧
// 白拉几倍候选。
func TestNewFetchPlanKeepsChunkGranularityOneToOne(t *testing.T) {
	plan := NewFetchPlan(Chunk, 20)

	if plan.Want() != 20 {
		t.Fatalf("目标条数应为 20，实际 %d", plan.Want())
	}
	if plan.Budget() != 20 {
		t.Fatalf("chunk 粒度下预算应等于条数 20，实际 %d", plan.Budget())
	}
}

// document 粒度下预算必须放大。
//
// 不放大就是「请求 20 篇、返回 4 篇」：一篇文档切成几块不由调用方决定，直接拿
// 条数当分块数，取回来的内容必然缩水几倍，而缩水的倍数还会随 chunkSize 变化。
func TestNewFetchPlanWidensForDocumentGranularity(t *testing.T) {
	plan := NewFetchPlan(Document, 20)

	if plan.Budget() != 20*initialFetchFactor {
		t.Fatalf("document 粒度下预算应为 %d，实际 %d", 20*initialFetchFactor, plan.Budget())
	}
	if plan.Budget() <= plan.Want() {
		t.Fatalf("预算 %d 不该小于等于目标 %d", plan.Budget(), plan.Want())
	}
}

// 预算有绝对上限：库里每篇文档被切成几十块时，翻倍会一路涨到几千，而每一次
// 翻倍都是一次完整的三通道检索。上限让轮数变成对数级、代价可算。
func TestNewFetchPlanCapsBudget(t *testing.T) {
	plan := NewFetchPlan(Document, 200)

	if plan.Budget() != maxChunkFetch {
		t.Fatalf("预算应封顶到 %d，实际 %d", maxChunkFetch, plan.Budget())
	}
	// 封顶之后不能小于目标：那会让「取满 200 篇」在一开始就不可能。
	if plan.Budget() < plan.Want() {
		t.Fatalf("预算 %d 小于目标 %d", plan.Budget(), plan.Want())
	}
}

// 没有目标（want <= 0）时不放大、也不该进入循环。
func TestNewFetchPlanWithoutTargetDoesNotWiden(t *testing.T) {
	plan := NewFetchPlan(Document, 0)

	if plan.Budget() != 0 {
		t.Fatalf("没有目标时预算应为 0（交给检索实现的默认值），实际 %d", plan.Budget())
	}
	if !plan.Settled(0, 0) {
		t.Fatal("没有目标时应直接收手")
	}
}

// 取满了就收手。
func TestSettledStopsWhenFilled(t *testing.T) {
	plan := NewFetchPlan(Document, 20) // 预算 80

	if !plan.Settled(20, 80) {
		t.Fatal("归并出 20 篇就该收手")
	}
	if !plan.Settled(21, 80) {
		t.Fatal("超过目标同样是取满")
	}
	if plan.Settled(19, 80) {
		t.Fatal("差一篇时应当继续加码")
	}
}

// **给不满预算就等于检索侧见底**，要立刻收手。
//
// 这一条是循环的刹车：库里只有 4 篇能匹配时，预算会一路翻到上限，每一轮都在问
// 一个已知没有更多内容的池子。少了它，一次「库里就 4 篇」的召回会白白多跑几轮
// 三通道检索，延迟翻几倍而结果一模一样。
func TestSettledStopsWhenRetrieverIsExhausted(t *testing.T) {
	plan := NewFetchPlan(Document, 20) // 预算 80

	if !plan.Settled(4, 20) {
		t.Fatal("只给了 20 块（少于预算 80）说明已经到底，应当收手")
	}
	// 边界：正好给满预算但没取满目标 —— 说明不了「到底了」，要继续加码。
	if plan.Settled(4, 80) {
		t.Fatal("给满了预算却没取满目标，该继续加码")
	}
}

// 预算到顶也收手：再翻只会更慢，不会更多。
func TestSettledStopsAtBudgetCap(t *testing.T) {
	plan := NewFetchPlan(Document, 200) // 预算已封顶 512

	if !plan.Settled(1, maxChunkFetch) {
		t.Fatal("预算到顶时应收手")
	}
}

// 加码是翻倍，并且到顶后明确报告「不能再加」—— 调用方拿到 false 必须直接收下
// 当前结果，而不是用同样的预算再拉一遍候选。
func TestWidenDoublesUntilCap(t *testing.T) {
	plan := NewFetchPlan(Document, 20) // 预算 80

	plan, ok := plan.Widen()
	if !ok || plan.Budget() != 160 {
		t.Fatalf("第一次加码应为 160，实际 %d（ok=%v）", plan.Budget(), ok)
	}
	plan, ok = plan.Widen()
	if !ok || plan.Budget() != 320 {
		t.Fatalf("第二次加码应为 320，实际 %d（ok=%v）", plan.Budget(), ok)
	}
	plan, ok = plan.Widen()
	if !ok || plan.Budget() != maxChunkFetch {
		t.Fatalf("第三次加码应封顶到 %d，实际 %d（ok=%v）", maxChunkFetch, plan.Budget(), ok)
	}
	if _, ok = plan.Widen(); ok {
		t.Fatal("已到顶时不该再报告可以加码")
	}
}

// 加码不能丢掉目标：条数是调用方要的，翻倍只改预算。
func TestWidenKeepsTarget(t *testing.T) {
	plan := NewFetchPlan(Document, 7)

	widened, ok := plan.Widen()
	if !ok {
		t.Fatal("7 篇的预算远未到顶，应当可以加码")
	}
	if widened.Want() != 7 {
		t.Fatalf("加码后目标应仍为 7，实际 %d", widened.Want())
	}
}

// 零值粒度按默认（document）处理：零值代表「最保守的读法」，不是「不放大」。
func TestNewFetchPlanTreatsZeroGranularityAsDefault(t *testing.T) {
	plan := NewFetchPlan("", 10)

	if plan.Budget() != 10*initialFetchFactor {
		t.Fatalf("零值粒度应按默认（%s）放大，实际预算 %d", Default, plan.Budget())
	}
}

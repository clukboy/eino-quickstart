package rag

import (
	"strings"
	"testing"

	"eino-quickstart/internal/platform/config"
)

func containsTerm(terms []string, want string) bool {
	for _, term := range terms {
		if term == want {
			return true
		}
	}
	return false
}

func TestTokenizeKeepsModelWhole(t *testing.T) {
	terms := Tokenize("H105P")

	if !containsTerm(terms, "h105p") {
		t.Fatalf("型号必须整体保留（小写化），实际 %v", terms)
	}
}

func TestTokenizeSplitsChineseIntoBigrams(t *testing.T) {
	terms := Tokenize("滑入式铰链")

	for _, want := range []string{"滑入", "入式", "式铰", "铰链"} {
		if !containsTerm(terms, want) {
			t.Fatalf("缺少 2-gram %q，实际 %v", want, terms)
		}
	}
	// 整串也作为一个词元：完整短语命中比单个 2-gram 更有分量。
	if !containsTerm(terms, "滑入式铰链") {
		t.Fatalf("缺少整串词元，实际 %v", terms)
	}
}

func TestTokenizeHandlesMixedScriptAndPunctuation(t *testing.T) {
	terms := Tokenize("H105P 二段力铰链")

	if !containsTerm(terms, "h105p") {
		t.Fatalf("缺少型号词元，实际 %v", terms)
	}
	if !containsTerm(terms, "二段") || !containsTerm(terms, "铰链") {
		t.Fatalf("缺少中文 2-gram，实际 %v", terms)
	}
	// 空格是分隔符，不该出现在任何词元里。
	for _, term := range terms {
		if strings.ContainsAny(term, " \t\n") && term != strings.ToLower("H105P 二段力铰链") {
			t.Fatalf("词元 %q 里残留了空白字符", term)
		}
	}
}

// LIKE 的通配符不是字面量，带进 SQL 会变成无意义的宽匹配，必须在词元层面丢掉。
func TestTokenizeDropsLikeWildcards(t *testing.T) {
	terms := Tokenize("100% 的 _ 参数")

	for _, term := range terms {
		if strings.ContainsAny(term, "%_\\") {
			t.Fatalf("词元 %q 含 LIKE 通配符", term)
		}
	}
}

func TestTokenizeEmptyInput(t *testing.T) {
	for _, input := range []string{"", "   ", "\t\n"} {
		if terms := Tokenize(input); len(terms) != 0 {
			t.Fatalf("空白输入应得到空词元，输入 %q 得到 %v", input, terms)
		}
	}
}

func TestTokenizeDeduplicates(t *testing.T) {
	terms := Tokenize("铰链 铰链")
	seen := make(map[string]int, len(terms))
	for _, term := range terms {
		seen[term]++
	}
	for term, count := range seen {
		if count > 1 {
			t.Fatalf("词元 %q 重复出现 %d 次", term, count)
		}
	}
}

// 长查询不做整串词元：它不可能作为子串命中，只会多带一个恒不成立的 LIKE 条件。
func TestTokenizeSkipsOverlongPhrase(t *testing.T) {
	long := strings.Repeat("铰链", maxPhraseRunes)
	terms := Tokenize(long)

	if containsTerm(terms, strings.ToLower(long)) {
		t.Fatalf("超过 %d 字的整串不该作为词元", maxPhraseRunes)
	}
	if !containsTerm(terms, "铰链") {
		t.Fatalf("2-gram 仍应存在，实际 %v", terms)
	}
}

// PolicyFromConfig 必须交出「真正会生效」的那份策略，而不是配置的原始值。
//
// 组合根会拿它做装配决策：向量库起不来时把 VectorWeight 置 0 关掉那条通道。如果
// 权重缺省时这里返回全 0，组合根看到的就是「向量已经是关的」，于是不会去关它；
// 而检索器侧 normalize 会把默认的 1 补回来 —— 通道又开了，且每次请求都往
// degraded 里填一条 vector。那条信息本来是给运维看异常的，一旦常驻就等于没有。
func TestPolicyFromConfigReturnsEffectiveWeights(t *testing.T) {
	// 配置里整段 retrieval 都省略：三个权重都是零值。
	policy := PolicyFromConfig(config.RetrievalConfig{})

	if policy.VectorWeight <= 0 {
		t.Fatalf("缺省权重应当补出默认值，实际 VectorWeight=%v", policy.VectorWeight)
	}
	if policy.ExactWeight <= policy.KeywordWeight {
		t.Errorf("精确通道的权重应当高于关键字通道：exact=%v keyword=%v",
			policy.ExactWeight, policy.KeywordWeight)
	}
	if policy.KeywordWeight <= policy.VectorWeight {
		t.Errorf("关键字通道的权重应当不低于向量通道：keyword=%v vector=%v",
			policy.KeywordWeight, policy.VectorWeight)
	}

	// 候选上限也一并补齐：为零会让每条通道取 0 条候选，融合出来永远是空结果。
	if policy.ExactCandidateLimit <= 0 ||
		policy.KeywordCandidateLimit <= 0 ||
		policy.VectorCandidateLimit <= 0 {
		t.Errorf("候选上限没有补出默认值：%+v", policy)
	}
	if policy.RRFSmoothing <= 0 {
		t.Errorf("RRF 平滑常数没有补出默认值：%v", policy.RRFSmoothing)
	}

	// 配置显式给出的值必须原样保留，不能被默认值覆盖。
	configured := PolicyFromConfig(config.RetrievalConfig{
		ExactWeight: 9, KeywordWeight: 3, VectorWeight: 0,
		RRFSmoothing: 12, ExactCandidateLimit: 7,
		KeywordCandidateLimit: 8, VectorCandidateLimit: 9,
	})
	if configured.ExactWeight != 9 {
		t.Errorf("显式权重被覆盖：ExactWeight=%v，期望 9", configured.ExactWeight)
	}
	// 关掉一条通道（权重 0）也要照原样执行，而不是被默认值打开。
	if configured.VectorWeight != 0 {
		t.Errorf("显式把向量权重置 0 应当保持关闭，实际 %v", configured.VectorWeight)
	}
	// 归一化必须能通过多次调用收敛：组合根改过一个字段后再算一次也应当稳定。
	if again := configured.normalize(); again != configured {
		t.Errorf("归一化不是幂等的：%+v → %+v", configured, again)
	}
}

package grouping

import (
	"strings"
	"testing"
)

func TestParseGranularity(t *testing.T) {
	cases := map[string]Granularity{
		"":            Default,
		"document":    Document,
		"chunk":       Chunk,
		"  DOCUMENT ": Document,
		"Chunk":       Chunk,
	}
	for raw, want := range cases {
		got, err := ParseGranularity(raw)
		if err != nil {
			t.Fatalf("ParseGranularity(%q) 报错: %v", raw, err)
		}
		if got != want {
			t.Errorf("ParseGranularity(%q) = %q，期望 %q", raw, got, want)
		}
	}
}

// 粒度名写错必须报错，不能静默回落到默认值：静默回落会让整轮评测的数字换一个
// 单位，而症状长得和「召回质量变了」一模一样。
func TestParseGranularityRejectsUnknownValue(t *testing.T) {
	for _, raw := range []string{"documents", "doc", "chunks", "分块"} {
		if _, err := ParseGranularity(raw); err == nil {
			t.Errorf("ParseGranularity(%q) 应当报错", raw)
		}
	}
}

func TestPolicyResolvesByDatasetType(t *testing.T) {
	policy, err := NewPolicy(map[string]string{
		DefaultKey: "chunk",
		"product":  "document",
	})
	if err != nil {
		t.Fatalf("构造策略失败: %v", err)
	}

	cases := map[string]Granularity{
		"product": Document,
		"text":    Chunk, // 没配过的类型走兜底
		"":        Chunk,
	}
	for datasetType, want := range cases {
		if got := policy.For(datasetType); got != want {
			t.Errorf("For(%q) = %q，期望 %q", datasetType, got, want)
		}
	}
}

// 零值可用：策略没配（配置里没有 recallGrouping 段）时不能 panic，也不能返回
// 空字符串 —— 空粒度会一路传到判定逻辑里，那里只会把它当成「不是 chunk」。
func TestPolicyZeroValueUsesDefault(t *testing.T) {
	var policy Policy

	if got := policy.For("product"); got != Default {
		t.Fatalf("零值策略应回落到 %q，实际 %q", Default, got)
	}
	if got := policy.For(""); got != Default {
		t.Fatalf("零值策略对空类型也应回落到 %q，实际 %q", Default, got)
	}
}

// 空键等价于 default：YAML 里用 "" 当默认值是很常见的写法，两种都得认。
func TestPolicyTreatsEmptyKeyAsFallback(t *testing.T) {
	policy, err := NewPolicy(map[string]string{"": "chunk"})
	if err != nil {
		t.Fatalf("构造策略失败: %v", err)
	}
	if got := policy.For("whatever"); got != Chunk {
		t.Fatalf("空键应被当作兜底，实际 %q", got)
	}
}

// 配置里的值在构造期就校验：失败发生在进程启动时，一眼能看到；留到查询期就
// 只会表现为「按默认粒度返回」。
func TestNewPolicyFailsFastOnBadValue(t *testing.T) {
	_, err := NewPolicy(map[string]string{"product": "documents"})
	if err == nil {
		t.Fatal("非法的粒度值应当让构造失败")
	}
	// 报错要指到具体是哪个键：配置里可能有好几行。
	if !strings.Contains(err.Error(), "product") {
		t.Fatalf("报错应包含出错的键名，实际 %q", err.Error())
	}
}

func TestGranularityLabel(t *testing.T) {
	if got := Chunk.Label(); got != "分块" {
		t.Errorf("Chunk.Label() = %q", got)
	}
	if got := Document.Label(); got != "文档" {
		t.Errorf("Document.Label() = %q", got)
	}
}

// Describe 是打进日志与终端的那一句，必须同时给出「用了哪个粒度」和「按哪个类型
// 查的」—— 只看结果的人要靠它判断这个数字是什么单位。
func TestPolicyDescribeNamesTypeAndGranularity(t *testing.T) {
	policy, err := NewPolicy(map[string]string{DefaultKey: "chunk", "product": "document"})
	if err != nil {
		t.Fatalf("构造策略失败: %v", err)
	}

	described := policy.Describe("product")
	for _, want := range []string{"document", "product"} {
		if !strings.Contains(described, want) {
			t.Errorf("Describe 缺少 %q: %s", want, described)
		}
	}
	// 类型为空时用 default 代替，不能出现「数据集类型 」这样的空档。
	if described := policy.Describe(""); !strings.Contains(described, DefaultKey) {
		t.Errorf("空类型应显示为 %s: %s", DefaultKey, described)
	}
}

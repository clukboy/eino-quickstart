// Package grouping 决定召回结果按什么粒度归并后返回。
//
// 这不是「显示格式」，而是召回契约的一部分：产品型录里一篇文档就是一个产品
// （型号、系列、规格与正文都在那一篇里），召回出来给人或给模型看的应当是文档；
// 普通文档库反过来，一篇长文切成两百块，合并成一条等于把两百条内容压成一条，
// 调用方什么也拿不到。
//
// 所以粒度由**知识库类型**决定，而不是写死在代码里 —— 一条检索链路要同时服务
// 两类库，把粒度钉死在其中一边，另一边必然错，而且错得不像 bug：症状是
// 「同一个产品在结果里出现八次」或者「明明有内容却只回一条」，两种都容易被
// 误读成召回质量问题。
package grouping

import (
	"fmt"
	"strings"
)

// Granularity 是召回结果的归并粒度。
type Granularity string

const (
	// Chunk 按分块原样返回：一条结果 = 一个分块。
	Chunk Granularity = "chunk"
	// Document 按文档归并：一条结果 = 一篇文档，取命中的最高分块作代表。
	Document Granularity = "document"
)

// Default 是没配 recallGrouping 时用的粒度。
//
// 取 Document：一篇文档才是人和下游模型真正消费的单位，分块是存储与检索的
// 实现细节。默认返回分块，意味着每个调用方都得自己做一遍归并，而做漏了的表现
// 是「同一个产品出现八次」—— 看起来像召回质量差，实际是粒度选错了。
const Default = Document

// DefaultKey 是配置里表示兜底那一项的键。
const DefaultKey = "default"

// Label 是给人看的短名，用来写进终端输出。
//
// 与 Granularity 本身（document / chunk）分开：那个值是配置与 JSON 里的稳定标识，
// 要能 grep、要能两次 run 直接 diff；这个是给人读的，中文里「按分块列出」比
// 「按 chunk 列出」好读。混用一个值会让「改文案」变成「改契约」。
func (g Granularity) Label() string {
	if g == Chunk {
		return "分块"
	}
	return "文档"
}

// ParseGranularity 解析一个粒度配置值。空字符串取 Default。
func ParseGranularity(value string) (Granularity, error) {
	switch Granularity(strings.ToLower(strings.TrimSpace(value))) {
	case "":
		return Default, nil
	case Chunk:
		return Chunk, nil
	case Document:
		return Document, nil
	default:
		return "", fmt.Errorf("未知的归并粒度 %q（可选 %s / %s）", value, Chunk, Document)
	}
}

// Policy 按数据集类型解析归并粒度。
//
// 零值可用：没有类型匹配时回落到 Default。
type Policy struct {
	byType   map[string]Granularity
	fallback Granularity
}

// NewPolicy 从配置构造策略。键是 dataset.type，DefaultKey 是兜底。
//
// 取值在**构造期**校验，不留到查询期：写错一个粒度名（"documents"、"doc"）如果
// 只在查询时才发现，表现会静默退化成「按默认粒度返回」，而不是报错 —— 那正是
// 最难查的一类配置事故。构造失败发生在进程启动时，一眼就能看到。
func NewPolicy(configured map[string]string) (Policy, error) {
	policy := Policy{byType: make(map[string]Granularity, len(configured)), fallback: Default}
	for key, raw := range configured {
		granularity, err := ParseGranularity(raw)
		if err != nil {
			return Policy{}, fmt.Errorf("recallGrouping[%s]: %w", key, err)
		}
		name := strings.TrimSpace(key)
		// 空键也当兜底：YAML 里写 "" 当默认值是常见写法，两个都认。
		if name == "" || strings.EqualFold(name, DefaultKey) {
			policy.fallback = granularity
			continue
		}
		policy.byType[name] = granularity
	}
	return policy, nil
}

// For 返回该数据集类型该用的粒度。类型没配过就用兜底值。
func (p Policy) For(datasetType string) Granularity {
	if granularity, ok := p.byType[strings.TrimSpace(datasetType)]; ok {
		return granularity
	}
	if p.fallback == "" {
		return Default
	}
	return p.fallback
}

// Describe 是给人看的一句话描述，用来写进报告与日志。
//
// 报告的用途是「几十秒内判断这次改动能不能合」，而同一份用例集在不同粒度下的
// 数字不可直接比较 —— 粒度必须和关键词通道一样被打在报告开头，否则报告一旦
// 离开当时的上下文，就没人知道那些条数是什么单位。
func (p Policy) Describe(datasetType string) string {
	name := strings.TrimSpace(datasetType)
	if name == "" {
		name = DefaultKey
	}
	return fmt.Sprintf("%s（数据集类型 %s）", p.For(datasetType), name)
}

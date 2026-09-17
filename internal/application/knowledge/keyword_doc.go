package knowledge

import (
	"strings"

	"eino-quickstart/internal/rag/constant"
)

// 这个文件是「分块的 metadata 列」在进入检索索引之前的最后一道加工：把两层
// 元数据合成一份，剔掉链路自身的机制键，并把空值清掉。
//
// 「哪些键单独成列、叫什么名字、什么类型、多少权重、值从哪个路径取」不在这里 ——
// 那些由映射文件声明（configs/es/*.yaml，见 internal/platform/storage/es 的
// mapping.go），因为它们是按索引变化的业务语义。这个文件只做与索引形态无关、
// 但对所有索引都成立的清洗。

// metadataExcluded 是不进检索索引的键。
//
// 排除的理由分三类：
//
//   - 在索引里已经有专门字段（source / title / heading_path），再摊平一遍只会
//     重复计分。
//   - 属于链路自身的机制数据（doc_id 是内容 hash、content_hash、chunk_index、
//     dataset_id）。它们要么是没有检索意义的随机串，要么是枚举值 —— 把
//     visibility=system 索引进去，搜「system」就会命中全库文档。
//   - 注入的溯源码（type、以及 loader 写的 _source / _extension / _file_name）。
//     _source 的值就是文件名，已经有独立字段承载，重复进兜底字段只会让「按文件名
//     搜」命中两次、并把它挤出 4096 字节的配额。这一类统一按下划线前缀拦，见
//     isProvenanceKey —— 逐条登记的话，loader 哪天再加一个 _xxx 又会漏。
//
// 剔除发生在进入 es 包之前，所以映射文件里的 from 只能指向业务键；这一点在
// es 包的 mapping.go 里有对应的说明。
var metadataExcluded = map[string]struct{}{
	constant.MetaDocID:       {},
	constant.MetaContentHash: {},
	constant.MetaVisibility:  {},
	constant.MetaOwner:       {},
	constant.MetaSource:      {},
	constant.MetaTitle:       {},
	constant.MetaHeadingPath: {},
	constant.MetaChunkIndex:  {},
	"dataset_id":             {},
	// 数据集类型（product / text）是 parser 注册表的查表键，不是给人搜的词。
	"type": {},
}

// isProvenanceKey 报告一个键是不是链路注入的溯源码。
//
// 约定：下划线开头的是 loader / parser 自己加的（_source / _extension /
// _file_name），业务元数据不用这个前缀。按下划线整类拦，比逐条登记更耐改。
func isProvenanceKey(key string) bool {
	return strings.HasPrefix(key, "_")
}

// searchableMetadata 合并两层元数据并清洗。
//
// 传多层是因为元数据分两处：分块自己那份（产品型录、规格明细，由 parser 从
// 产品块的 YAML 头写入）和文档那份（上传时调用方带的键）。两处都属于「这条
// 分块的内容」，都该被搜到。同名的键由后一层覆盖前一层 —— 调用方把更贴近内容
// 的那层放在后面。
//
// 空值在这里就清掉，而不是留给索引侧：写入端会把 map 原样交给映射声明取值，
// 一个值为空串的键会走完整条链路，最后表现为「字段存在但永远为空」，排查时
// 很难分辨是「parser 没产出」还是「映射的路径写错了」。
func searchableMetadata(layers ...map[string]any) map[string]any {
	merged := make(map[string]any)
	for _, metadata := range layers {
		for key, value := range metadata {
			if isProvenanceKey(key) {
				continue
			}
			if _, skip := metadataExcluded[key]; skip {
				continue
			}
			normalized, ok := normalizeValue(value)
			if !ok {
				continue
			}
			merged[key] = normalized
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// normalizeValue 清掉一个值里的空壳，报告它是否还值得索引。
//
// 只处理三种情况：空标量、全空的数组、空的嵌套 map。数字与布尔一律保留 ——
// 「开合角度 0」和「是否带缓冲 false」都是有意义的值，过滤掉它们等于丢数据。
func normalizeValue(value any) (any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case string:
		text := strings.TrimSpace(typed)
		return text, text != ""
	case []string:
		compacted := compactStrings(typed)
		if len(compacted) == 0 {
			return nil, false
		}
		return compacted, true
	case []any:
		compacted := make([]any, 0, len(typed))
		for _, item := range typed {
			normalized, ok := normalizeValue(item)
			if ok {
				compacted = append(compacted, normalized)
			}
		}
		if len(compacted) == 0 {
			return nil, false
		}
		return compacted, true
	case map[string]any:
		if len(typed) == 0 {
			return nil, false
		}
		return typed, true
	default:
		// 数字、布尔、以及带类型的 nil 指针（*int(nil)）：一律保留。
		// 带类型的 nil 由索引侧的取值逻辑判空（见 es 包的 deref），在这里
		// 判定它需要反射，而漏判的后果只是多一个空字段。
		return typed, true
	}
}

// compactStrings 去掉字符串数组里的空白项。
//
// 全空的数组归一成空切片而不是 nil，由调用方据此丢掉整个键：`variants: []`
// 在产品型录里很常见（这个型号没有变体），把它当成「有值」写进索引只会多出
// 一个永远为空的字段。
func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			out = append(out, text)
		}
	}
	return out
}

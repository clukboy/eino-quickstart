package es

import (
	"encoding/json"
	"fmt"
	"time"
)

// 索引里的字段名。它们是写入与查询两侧共同的契约，抽成常量而不是散在
// 字符串里：共有字段的属性表、bulk 写入、multi_match 的字段名必须一模一样，
// 拼错一个字段不会报错，只会让那一路的权重静默失效。
//
// 业务字段名不在这一组里：它们由映射文件声明（见 mapping.go），是配置而不是
// 代码契约。
const (
	fieldChunkID      = "chunk_id"
	fieldDocumentID   = "document_id"
	fieldDatasetID    = "dataset_id"
	fieldChunkIndex   = "chunk_index"
	fieldSource       = "source"
	fieldTitle        = "title"
	fieldHeadingPath  = "heading_path"
	fieldContent      = "content"
	fieldVisibility   = "visibility"
	fieldOwner        = "owner"
	fieldIndexedAt    = "indexed_at"
	fieldMappingRev   = "mapping_rev"
	fieldMetadataText = "metadata_text"
)

// ChunkDoc 是一条分块在检索索引里的形态。
//
// 它刻意是「检索视图」而不是 document_chunks 行的复制：索引只需要知道
// 「按哪些字段打分」「命中之后回哪里取原文」，所以 pending / indexed 这类
// 状态机字段（content_hash、vector_status）留在 PostgreSQL，不进索引 ——
// 状态在两边各存一份，就必然会有对不上的那一天。
//
// 字段分两类：
//
//	共有字段  结构体上直接声明，值由调用方从 ent 实体填。
//	业务字段  调用方把整份元数据塞进 Metadata，写入时由映射声明决定取哪些、
//	          叫什么名字、什么类型（见 Mapping.Extract）。
//
// 之所以业务字段不在这里逐个声明：换一套产品线（字段完全不同）时，逐个声明
// 意味着改代码、发版、重建索引；而它们本来就是「配置描述的业务语义」。
//
// 字段类型取舍：
//   - 三个 ID 与 visibility / owner 存字符串、映射为 keyword。ES 的 long 也能
//     做精确匹配，但 keyword 不参与数值范围查询、不受动态映射干扰，做过滤更稳。
//   - content / title / heading_path / source 是可打分的 text，走配置的中文分词器。
type ChunkDoc struct {
	// ChunkID 同时是索引文档的 _id。bulk 用的是 index 动作，_id 相同即覆盖，
	// 所以同一批分块重复写入天然幂等，不会多出重复文档。
	ChunkID string `json:"chunk_id"`

	DocumentID  string    `json:"document_id"`
	DatasetID   string    `json:"dataset_id"`
	ChunkIndex  int       `json:"chunk_index"`
	Source      string    `json:"source"`
	Title       string    `json:"title"`
	HeadingPath string    `json:"heading_path"`
	Content     string    `json:"content"`
	Visibility  string    `json:"visibility,omitempty"`
	Owner       string    `json:"owner,omitempty"`
	IndexedAt   time.Time `json:"indexed_at"`

	// Metadata 是这条分块的业务元数据（产品型录、规格明细、上传时带的键）。
	//
	// 它不进 _source 本身，而是被映射声明取走：声明里有的键单独成列，其余的
	// 摊平进 MetadataText。调用方在这一步之前就该把链路机制键摘掉 ——
	// doc_id 是内容 hash、visibility 是枚举值，把它们索引进去，搜「system」
	// 会命中全库（见 application/knowledge 的 searchableMetadata）。
	Metadata map[string]any `json:"-"`

	// MetadataText 是**没单独成列**的那部分元数据的摊平结果，由写入端填充。
	//
	// 「没单独成列」是硬约束：已经进了业务字段的值不再收进来。同一个值同时出现在
	// 一个高权重字段和一个兜底字段里，除了把 tf 抬高、扰乱排序之外没有任何作用
	// （见 Mapping.Project）。
	MetadataText string `json:"metadata_text,omitempty"`

	// MappingRev 是写入时映射的形态指纹，由写入端盖章（见 Mapping.fingerprint），
	// 调用方不需要设置。
	//
	// 它取代了手写的形态版本号：映射从代码搬到配置文件之后，人工维护的版本号
	// 就成了第二处需要同步的地方，忘了同步的表现是「升级之后按型号搜不到东西」
	// 而预检不会提示需要 reindex。指纹由声明本身算出来，改了就变。
	MappingRev string `json:"mapping_rev"`

	// extra 是按映射声明提取出的业务字段值，由写入端填充并在序列化时摊平进
	// 文档顶层。它不出现在结构体字段里是因为字段名是配置项，不是编译期常量。
	extra map[string]any
}

// MarshalJSON 把业务字段摊平到文档顶层。
//
// 走「先序列化共有字段、再合并业务字段」而不是直接构造 map，是为了让共有字段
// 的 JSON 形态仍然由结构体标签单点定义 —— 两处各写一份的话，改标签时漏掉一处
// 就会出现「同一条分块按调用路径不同写成两种文档」。
func (d ChunkDoc) MarshalJSON() ([]byte, error) {
	// alias 躲开本方法自己造成的无限递归（Extra 已经用 json:"-" 排除在序列化外）。
	type alias ChunkDoc
	encoded, err := json.Marshal(alias(d))
	if err != nil {
		return nil, fmt.Errorf("es: 序列化分块 %s: %w", d.ChunkID, err)
	}
	if len(d.extra) == 0 {
		return encoded, nil
	}

	merged := make(map[string]json.RawMessage, len(d.extra)+16)
	if err := json.Unmarshal(encoded, &merged); err != nil {
		return nil, fmt.Errorf("es: 序列化分块 %s: %w", d.ChunkID, err)
	}
	for name, value := range d.extra {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("es: 序列化分块 %s 的字段 %s: %w", d.ChunkID, name, err)
		}
		merged[name] = raw
	}
	// map 的序列化顺序由 encoding/json 保证按键升序，写入的文档因此是确定的。
	return json.Marshal(merged)
}

// enrich 按映射把元数据落进文档：声明的字段单独成列，其余的摊平进兜底字段。
//
// 由写入端（Client.IndexChunks）调用而不是让调用方自己提取：提取规则属于映射，
// 而映射只有写入端持有 —— 让调用方提取意味着它得先拿到映射，也就意味着
// 「哪些字段存在」这件事会在多个进程里各有一份理解。
//
// 取值与摊平是同一次判断的两面（Mapping.Project）：分成两步做的话，已经单独成列
// 的值会被兜底字段再收一遍，同一个词在两个权重不同的字段里各记一次，把排序弄坏。
func (d *ChunkDoc) enrich(mapping *Mapping, rev string) {
	d.MappingRev = rev
	if d.Metadata == nil {
		return
	}
	columns, fallback := mapping.Project(d.Metadata)
	d.extra = columns
	d.MetadataText = fallback
}

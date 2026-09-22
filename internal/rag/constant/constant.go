package constant

// Well-known metadata keys stored on *schema.Document between pipeline stages.
// These mirror the provenance data the pipeline needs to produce citations and
// to respect document ACL/visibility downstream.
const (
	MetaSource      = "source"       // 文档的逻辑标识（不是磁盘路径，见 rag.DocumentSource）
	MetaTitle       = "title"        // 文档标题（文件名或一级标题）
	MetaDocID       = "doc_id"       // 文档唯一 ID（内容 hash 派生）
	MetaHeadingPath = "heading_path" // chunk 所属标题路径，如 "安装 > 依赖"
	MetaChunkIndex  = "chunk_index"  // chunk 在文档内的序号
	MetaContentHash = "content_hash" // 正文 hash，worker 用它判断分块是不是这份正文切出来的
	// MetaSpecHash 是产品块（正文 + YAML 头解析出的键）的指纹，由写入路径算并落在
	// documents.metadata 上，用来回答「这次重传的内容和一个版本比变了没有」。
	//
	// 与 MetaContentHash 的区别是它**包含元数据**：只改了 YAML 头的那种重传，
	// 正文一字未动，只看 content_hash 会判成「没变」，元数据就永远停在旧值上。
	MetaSpecHash   = "spec_hash"
	MetaVisibility = "visibility" // ACL: public / internal / private
	MetaOwner      = "owner"      // ACL: 属主
)

// 产品型录元数据：由 internal/rag/parser/product.go 从产品块的 YAML 头写进
// chunk 的 MetaData。
//
// 单独列出来是因为它们的用途和上面那组不同 —— 上面那组是链路自身的溯源信息，
// 这一组是**业务语义**，而且几乎是「按型号找资料」这类查询唯一的命中面：型号、
// 系列、品类通常只出现在 YAML 头里，正文里一次都不出现。检索侧（ES 索引的
// mapping 与打分字段）按这些键取值，键名写错不会报错，只会让那一路静默失效。
const (
	MetaProductID    = "product_id"
	MetaModel        = "model"
	MetaFamilyPrefix = "family_prefix"
	MetaSeriesName   = "series_name"
	MetaProductName  = "product_name"
	MetaCategoryL1   = "category_l1"
	MetaCategoryL2   = "category_l2"
	MetaSpecsFromDoc = "specs_from_doc"
	MetaVariants     = "variants"
)

package constant

// Well-known metadata keys stored on *schema.Document between pipeline stages.
// These mirror the provenance data the pipeline needs to produce citations and
// to respect document ACL/visibility downstream.
const (
	MetaSource      = "source"       // 文件路径/URI
	MetaTitle       = "title"        // 文档标题（文件名或一级标题）
	MetaDocID       = "doc_id"       // 文档唯一 ID（内容 hash 派生）
	MetaHeadingPath = "heading_path" // chunk 所属标题路径，如 "安装 > 依赖"
	MetaChunkIndex  = "chunk_index"  // chunk 在文档内的序号
	MetaContentHash = "content_hash" // 原文内容 hash，用于幂等入库
	MetaVisibility  = "visibility"   // ACL: public / internal / private
	MetaOwner       = "owner"        // ACL: 属主
)

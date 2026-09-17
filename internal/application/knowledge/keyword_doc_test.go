package knowledge

import (
	"strings"
	"testing"

	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/internal/platform/storage/es"
)

// hingeMetadata 是线上真实的一份产品元数据（H105P / 图冠系列）。
//
// 断言用它而不是编一份干净的样例，是因为这里的取值形状全都有讲究：规格是数值
// （open_angle_deg: 100 不是 "100"）、variants 是空数组、specs_from_doc 是嵌套一层
// map、yaml 与 json 解出来的 map 类型不同（map[string]any vs map[string]interface{}）。
// 编造的样例会不自觉地把它们写成整齐的字符串，正好绕过所有类型分支。
func hingeMetadata() map[string]any {
	return map[string]any{
		"product_id":    "H105P",
		"model":         "H105P",
		"family_prefix": "H105",
		"series_name":   "图冠系列",
		"product_name":  "二段力小角度偏心轮快装缓冲铰链",
		"category_l1":   "铰链",
		"category_l2":   "缓冲铰链",
		"specs_from_doc": map[string]any{
			"mechanics_type":        "二段力",
			"install_type":          "快装",
			"adjust_type":           "偏心轮",
			"door_material":         "木门",
			"base_material":         "冷轧钢",
			"surface_finish":        "镀钛镍",
			"open_angle_deg":        100,
			"cup_diameter_mm":       35,
			"door_thickness_min_mm": 15,
			"door_thickness_max_mm": 28,
		},
		"variants":   []any{},
		"source_doc": "图特知识库模版铰链-新(1)(1).docx",

		// 链路自己注入的机制键。它们必须被排除，否则「搜 system 命中全库」
		// 这类问题会以「召回率突然变高」的形式出现，很难联想到是索引里多了
		// 一个枚举值。
		"doc_id":       uint64(11),
		"dataset_id":   uint64(7),
		"visibility":   "system",
		"owner":        "developer-1",
		"source":       "H105P.md",
		"heading_path": "规格 > 参数",
		"chunk_index":  0,
		"type":         "product",
	}
}

// TestSearchableMetadataSkipsMachinery 守住「机制键不进检索索引」这条约定。
//
// 它们的值要么是没有检索意义的随机串（doc_id 是内容 hash），要么是枚举值 ——
// 把 visibility=system 索引进去，搜「system」就会命中全库文档。
func TestSearchableMetadataSkipsMachinery(t *testing.T) {
	metadata := searchableMetadata(hingeMetadata())

	for _, key := range []string{
		"product_id", "model", "series_name", "specs_from_doc", "source_doc",
	} {
		if _, ok := metadata[key]; !ok {
			t.Errorf("业务键 %q 被误删了", key)
		}
	}
	for _, key := range []string{
		"doc_id", "dataset_id", "visibility", "owner",
		"source", "heading_path", "chunk_index", "type",
	} {
		if _, ok := metadata[key]; ok {
			t.Errorf("机制键 %q 不该进检索索引", key)
		}
	}
}

// TestSearchableMetadataDropsEmptyValues 钉住「空值在进入索引前就清掉」。
//
// 不清的后果是「字段存在但永远为空」—— 那正是映射路径写错时的表现，两者混在
// 一起会让排查方向完全跑偏。
func TestSearchableMetadataDropsEmptyValues(t *testing.T) {
	metadata := searchableMetadata(map[string]any{
		"model":    "",
		"note":     "   ",
		"variants": []any{},
		"series":   []any{"", "  "},
		"nested":   map[string]any{},
		// 数字与布尔要留下：开合角度 0 与「是否带缓冲 false」都是有意义的值。
		"angle":    0,
		"buffered": false,
	})

	for _, key := range []string{"model", "note", "variants", "series", "nested"} {
		if _, ok := metadata[key]; ok {
			t.Errorf("空值 %q 应当被丢掉，实际留下 %#v", key, metadata[key])
		}
	}
	for _, key := range []string{"angle", "buffered"} {
		if _, ok := metadata[key]; !ok {
			t.Errorf("非空的零值 %q 不该被丢掉", key)
		}
	}
}

// TestSearchableMetadataMergesLayers 覆盖两层元数据合并：文档那份在前、
// 分块那份在后，同名键由分块覆盖 —— 它比文档级的值更贴近这一块内容。
func TestSearchableMetadataMergesLayers(t *testing.T) {
	documentLayer := map[string]any{"tier": "外部可查", "model": "旧型号", "source": "H105P.md"}
	chunkLayer := map[string]any{"model": "H105P", "doc_id": uint64(11)}

	metadata := searchableMetadata(documentLayer, chunkLayer)
	if metadata["model"] != "H105P" {
		t.Errorf("分块层的值没有覆盖文档层: %#v", metadata["model"])
	}
	if metadata["tier"] != "外部可查" {
		t.Error("文档级的业务键应当保留（上传时调用方带的键也属于这条分块）")
	}
	if _, ok := metadata["source"]; ok {
		t.Error("机制键在合并时也该被剔除")
	}
	if metadata := searchableMetadata(nil, nil); metadata != nil {
		t.Errorf("没有任何可索引的键时应当返回 nil，实际 %#v", metadata)
	}
}

// TestMetadataFallsBackToCatchAll 是「规格明细也能被搜到」的核心断言。
//
// 规格值在嵌套 map 里，最容易被「只处理顶层字符串」的实现漏掉，而「门板材质是
// 冷轧钢」这类恰恰是用户会输入的词。
func TestMetadataFallsBackToCatchAll(t *testing.T) {
	text := es.FlattenMetadata(searchableMetadata(hingeMetadata()))

	for _, want := range []string{
		"二段力", "快装", "偏心轮", "木门", "冷轧钢", "镀钛镍",
		"100", "35", "15", "28",
		"图特知识库模版铰链-新(1)(1).docx",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("兜底字段里缺少 %q: %s", want, text)
		}
	}
	for _, unwanted := range []string{
		"system",      // visibility：枚举值进了索引，搜它就会命中全库
		"developer-1", // owner
		"H105P.md",    // source：已有独立字段
		"规格 > 参数",     // heading_path：已有独立字段
		"product",     // 数据集类型
	} {
		if strings.Contains(text, unwanted) {
			t.Errorf("兜底字段里不该出现 %q: %s", unwanted, text)
		}
	}
}

// TestBuildChunkDocsSeparatesDocumentAndChunkSources 钉住一条容易混淆的边界：
// 文档级字段（title / source / ACL）取 ent 的当前值，元数据只从 metadata 列读。
//
// 反过来做（从分块的 metadata 快照读 title / visibility）的后果是：文档改名或
// 转私有之后，索引里还是旧值，而检索侧要拿这些值做引用与权限判断。
func TestBuildChunkDocsSeparatesDocumentAndChunkSources(t *testing.T) {
	heading := "规格 > 参数"
	doc := &ent.Document{
		ID:           11,
		DatasetID:    7,
		Source:       "H105P.md",
		Title:        "H105P 二段力铰链",
		OwnerSubject: "developer-1",
		Visibility:   document.VisibilitySystem,
		// 文档级元数据：改名之前的旧标题留在这里，检索必须取 ent 现值。
		Metadata: map[string]any{"tier": "外部可查", "title": "旧标题"},
	}
	chunks := []*ent.DocumentChunk{{
		ID:          5,
		ChunkIndex:  0,
		Content:     "H105P 的安装孔距 35mm",
		HeadingPath: &heading,
		Metadata:    hingeMetadata(),
	}}

	docs := buildChunkDocs(doc, chunks)
	if len(docs) != 1 {
		t.Fatalf("分块数量是 %d，期望 1", len(docs))
	}
	indexed := docs[0]

	if indexed.ChunkID != "5" || indexed.DocumentID != "11" || indexed.DatasetID != "7" {
		t.Errorf("ID 字段错误: %+v", indexed)
	}
	if indexed.Source != "H105P.md" || indexed.Title != "H105P 二段力铰链" {
		t.Errorf("文档级字段没有取 ent 现值: %+v", indexed)
	}
	if indexed.Visibility != "system" || indexed.Owner != "developer-1" {
		t.Errorf("ACL 字段没有取 ent 现值: %+v", indexed)
	}
	if indexed.HeadingPath != heading {
		t.Errorf("heading_path 是 %q，期望 %q", indexed.HeadingPath, heading)
	}
	// 机制键不该被带进文档的元数据。
	for _, key := range []string{"doc_id", "visibility", "type"} {
		if _, ok := indexed.Metadata[key]; ok {
			t.Errorf("机制键 %q 被带进了检索文档", key)
		}
	}
	if indexed.Metadata["tier"] != "外部可查" {
		t.Error("文档级元数据应当合并进来（上传时带的键也属于这条分块）")
	}
	// 产品字段由映射声明取值，这里用发给运维的那份映射验一遍全链路。
	values := loadShippedMapping(t).Extract(indexed.Metadata)
	for field, want := range map[string]any{
		"model":       "H105P",
		"series_name": "图冠系列",
		"category_l2": "缓冲铰链",
	} {
		if values[field] != want {
			t.Errorf("字段 %s 的值是 %#v，期望 %#v", field, values[field], want)
		}
	}
	// 空 variants 不该产出字段。
	if _, ok := values["variants"]; ok {
		t.Errorf("空的变体数组不该产出字段: %#v", values["variants"])
	}
}

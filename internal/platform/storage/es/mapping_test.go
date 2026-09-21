package es

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 这个文件守住「声明层」的两件事：加载时能不能拦住写错的声明，以及声明能不能
// 正确地从元数据里取到值。两件事的失败方式都是静默的 —— 前者让字段永远为空，
// 后者让字段永远为空或塞进错误的值 —— 所以只能靠断言。

// TestLoadMappingAcceptsYAMLAndJSON 钉住两种格式等价。
//
// 收两种格式是为了迁就两边的既有习惯（ES 原生是 JSON，产品元数据源是 YAML），
// 但它们必须是同一份语义：不一样的话，「本地用 YAML 调通了、生产换 JSON 变了
// 行为」这种事故会非常难查。
func TestLoadMappingAcceptsYAMLAndJSON(t *testing.T) {
	yamlMapping, err := LoadMapping(shippedMappingPath)
	if err != nil {
		t.Fatalf("加载 YAML 映射: %v", err)
	}
	if yamlMapping.Name == "" {
		t.Error("映射应当有名字，日志与报错要靠它指明用的是哪一份")
	}

	// 把已加载的结果写成 JSON 再加载一次：断言的是「两条解析路径产出同一份
	// 声明」，而不是某个手抄副本与原文一致 —— 副本一定会漂移。
	encoded, err := json.Marshal(yamlMapping)
	if err != nil {
		t.Fatalf("序列化映射: %v", err)
	}
	jsonPath := filepath.Join(t.TempDir(), "chunk_mapping.json")
	if err := os.WriteFile(jsonPath, encoded, 0o600); err != nil {
		t.Fatalf("写入 JSON 映射: %v", err)
	}
	jsonMapping, err := LoadMapping(jsonPath)
	if err != nil {
		t.Fatalf("加载 JSON 映射: %v", err)
	}

	if got, want := strings.Join(jsonMapping.SearchFields(), ","),
		strings.Join(yamlMapping.SearchFields(), ","); got != want {
		t.Errorf("两种格式派生的打分字段不同:\n  JSON: %s\n  YAML: %s", got, want)
	}
	if got, want := jsonMapping.fingerprint("ik_max_word"), yamlMapping.fingerprint("ik_max_word"); got != want {
		t.Errorf("两种格式的形态指纹不同: %s != %s", got, want)
	}
}

// TestLoadMappingRejectsUnknownKey 守住「多打一个字母」这类错误。
//
// 未知键被忽略的话，`boostt: 5` 会变成「字段建了列、没有值、也不参与打分」，
// 而配置作者会以为自己配对了 —— 症状是检索质量差，不是报错。
func TestLoadMappingRejectsUnknownKey(t *testing.T) {
	path := writeMappingFile(t, `
name: bad
fields:
  - name: model
    from: model
    type: text
    boostt: 5
`)
	_, err := LoadMapping(path)
	if err == nil {
		t.Fatal("未知键必须报错")
	}
	if !strings.Contains(err.Error(), "boostt") {
		t.Errorf("错误信息应当点出未知键，实际: %v", err)
	}
}

// TestValidateRejectsDeclarationsESWouldAccept 逐条覆盖「ES 会接受、但结果不是
// 你想要的」那些声明。
func TestValidateRejectsDeclarationsESWouldAccept(t *testing.T) {
	cases := map[string]struct {
		mapping Mapping
		want    string
	}{
		"与共有字段重名": {
			mapping: Mapping{Fields: []FieldSpec{{Name: "content", From: "x", Type: FieldText}}},
			want:    "共有字段",
		},
		"字段名带点": {
			// ES 会把 a.b 建成 a 对象下的 b 字段，而写入端写的是平铺的键 ——
			// 结果是「字段存在但没人写值」和「值在别处」，两个都不报错。
			mapping: Mapping{Fields: []FieldSpec{{Name: "specs.angle", From: "x", Type: FieldText}}},
			want:    "不合法",
		},
		"重复声明": {
			mapping: Mapping{Fields: []FieldSpec{
				{Name: "model", From: "model", Type: FieldText},
				{Name: "model", From: "model", Type: FieldText},
			}},
			want: "重复",
		},
		"缺少 from": {
			mapping: Mapping{Fields: []FieldSpec{{Name: "model", Type: FieldText}}},
			want:    "缺少 from",
		},
		"未知类型": {
			mapping: Mapping{Fields: []FieldSpec{{Name: "model", From: "model", Type: "textt"}}},
			want:    "未知字段类型",
		},
		"数值字段给权重": {
			// multi_match 打在数值字段上只会静默贡献 0 分。
			mapping: Mapping{Fields: []FieldSpec{
				{Name: "open_angle_deg", From: "specs.open_angle_deg", Type: FieldInteger, Boost: 2},
			}},
			want: "不能参与 BM25 打分",
		},
		"keyword 类型挂子字段": {
			mapping: Mapping{Fields: []FieldSpec{
				{Name: "model", From: "model", Type: FieldKeyword, Keyword: true},
			}},
			want: "keyword 子字段",
		},
		"非 text 指定分词器": {
			mapping: Mapping{Fields: []FieldSpec{
				{Name: "model", From: "model", Type: FieldKeyword, Analyzer: "ik_max_word"},
			}},
			want: "analyzer",
		},
		"数值字段声明数组": {
			mapping: Mapping{Fields: []FieldSpec{
				{Name: "angles", From: "angles", Type: FieldInteger, Multi: true},
			}},
			want: "单值类型",
		},
		"负权重": {
			mapping: Mapping{Fields: []FieldSpec{{Name: "model", From: "model", Type: FieldText, Boost: -1}}},
			want:    "不能为负",
		},
		"baseBoost 指向不存在的字段": {
			mapping: Mapping{BaseBoost: map[string]float64{"modell": 3}},
			want:    "baseBoost",
		},
		"baseBoost 指向不可打分的字段": {
			mapping: Mapping{BaseBoost: map[string]float64{fieldChunkID: 3}},
			want:    "baseBoost",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := testCase.mapping.Validate()
			if err == nil {
				t.Fatal("应当报错")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("错误信息里没有 %q，实际: %v", testCase.want, err)
			}
		})
	}
}

// TestExtractPullsValuesByPath 用真实产品元数据的形状钉住取值。
//
// 这份元数据的三种形态各有坑：数值型规格（YAML 里是 int，经 JSON 列读回来是
// float64）、嵌套一层的规格明细、以及既是数组又可能是单值的型号变体。
func TestExtractPullsValuesByPath(t *testing.T) {
	mapping := &Mapping{Fields: []FieldSpec{
		{Name: "model", From: "model", Type: FieldText, Boost: 5},
		{Name: "product_id", From: "product_id", Type: FieldText, Boost: 5},
		{Name: "series_name", From: "series_name", Type: FieldText, Boost: 3},
		{Name: "variants", From: "variants", Type: FieldText, Multi: true},
		{Name: "surface_finish", From: "specs_from_doc.surface_finish", Type: FieldText},
		{Name: "open_angle_deg", From: "specs_from_doc.open_angle_deg", Type: FieldInteger},
		{Name: "cup_diameter_mm", From: "specs_from_doc.cup_diameter_mm", Type: FieldInteger},
		{Name: "note", From: "specs_from_doc.note", Type: FieldText},
	}}

	values := mapping.Extract(map[string]any{
		// 带引号的和不带引号的型号都要读出来：YAML 里 product_id 不加引号时
		// 解出来可能是数字。
		"product_id":  105,
		"model":       " H105P ",
		"series_name": "图冠系列",
		"variants":    []any{"H105P-白", "", "H105P-黑"},
		"specs_from_doc": map[string]any{
			"surface_finish":  "镀钛镍",
			"open_angle_deg":  float64(100),
			"cup_diameter_mm": 35,
		},
	})

	want := map[string]any{
		"model":           "H105P",
		"product_id":      "105",
		"series_name":     "图冠系列",
		"variants":        []any{"H105P-白", "H105P-黑"},
		"surface_finish":  "镀钛镍",
		"open_angle_deg":  int64(100),
		"cup_diameter_mm": int64(35),
	}
	for field, expected := range want {
		got, ok := values[field]
		if !ok {
			t.Errorf("字段 %s 没有被取到", field)
			continue
		}
		if !equalValue(got, expected) {
			t.Errorf("字段 %s 的值是 %#v，期望 %#v", field, got, expected)
		}
	}
	// 路径取不到就跳过，而不是写个空值进去。
	if _, ok := values["note"]; ok {
		t.Error("取不到的路径不该产出字段")
	}
}

// TestExtractAcceptsScalarForArrayField 覆盖「只有一个变体」的情形：
// YAML 里解出来是 string 而不是数组，直接断言会静默丢掉这个变体。
func TestExtractAcceptsScalarForArrayField(t *testing.T) {
	mapping := &Mapping{Fields: []FieldSpec{
		{Name: "variants", From: "variants", Type: FieldText, Multi: true},
	}}
	values := mapping.Extract(map[string]any{"variants": "H105P-白"})
	got, ok := values["variants"].([]any)
	if !ok || len(got) != 1 || got[0] != "H105P-白" {
		t.Fatalf("单值变体没有被规整成数组: %#v", values)
	}
}

// TestFingerprintTracksShapeNotBoost 钉住指纹的口径。
//
// 它是「哪些文档需要 reindex」的判据，所以只能包含索引期形态：权重是查询期的
// 东西（multi_match 的 boost），改权重却让全库文档变「陈旧」，会逼出一次完全
// 不必要的全量重建。
func TestFingerprintTracksShapeNotBoost(t *testing.T) {
	base := &Mapping{Fields: []FieldSpec{
		{Name: "model", From: "model", Type: FieldText, Boost: 5},
	}}

	boosted := &Mapping{Fields: []FieldSpec{
		{Name: "model", From: "model", Type: FieldText, Boost: 9},
	}}
	if base.fingerprint("ik_max_word") != boosted.fingerprint("ik_max_word") {
		t.Error("改权重不该改变形态指纹（它只影响查询期）")
	}
	baseBoostChanged := &Mapping{
		Fields:    base.Fields,
		BaseBoost: map[string]float64{fieldTitle: 9},
	}
	if base.fingerprint("ik_max_word") != baseBoostChanged.fingerprint("ik_max_word") {
		t.Error("改共有字段的权重不该改变形态指纹")
	}

	for name, changed := range map[string]*Mapping{
		"改类型":   {Fields: []FieldSpec{{Name: "model", From: "model", Type: FieldKeyword, Boost: 5}}},
		"改取值路径": {Fields: []FieldSpec{{Name: "model", From: "model_name", Type: FieldText, Boost: 5}}},
		"改子字段":  {Fields: []FieldSpec{{Name: "model", From: "model", Type: FieldText, Boost: 5, Keyword: true}}},
		"改数组标记": {Fields: []FieldSpec{{Name: "model", From: "model", Type: FieldText, Boost: 5, Multi: true}}},
		"加字段":   {Fields: []FieldSpec{{Name: "model", From: "model", Type: FieldText, Boost: 5}, {Name: "x", From: "x", Type: FieldText}}},
	} {
		if base.fingerprint("ik_max_word") == changed.fingerprint("ik_max_word") {
			t.Errorf("%s 必须改变形态指纹，否则升级后不会提示 reindex", name)
		}
	}

	// 字段集合相同、只是顺序不同时指纹必须一致：配置里挪一行就让全库文档变
	// 「陈旧」，会逼出一次完全不必要的全量重建。
	twoA := &Mapping{Fields: []FieldSpec{
		{Name: "model", From: "model", Type: FieldText, Boost: 5},
		{Name: "brand", From: "brand", Type: FieldText},
	}}
	twoB := &Mapping{Fields: []FieldSpec{
		{Name: "brand", From: "brand", Type: FieldText},
		{Name: "model", From: "model", Type: FieldText, Boost: 5},
	}}
	if twoA.fingerprint("ik_max_word") != twoB.fingerprint("ik_max_word") {
		t.Error("声明顺序不该改变形态指纹")
	}

	// 分词器变了会让索引里已有的词元与新配置对不上，同样必须变。
	if base.fingerprint("ik_max_word") == base.fingerprint("cjk") {
		t.Error("改分词器必须改变形态指纹")
	}
}

// TestSearchFieldsOrderAndBoosts 钉住字段表的形状：业务字段按声明顺序在前，
// 共有字段按固定顺序在后；权重 1 不写后缀。
func TestSearchFieldsOrderAndBoosts(t *testing.T) {
	mapping := &Mapping{
		Fields: []FieldSpec{
			{Name: "model", From: "model", Type: FieldText, Boost: 5},
			{Name: "open_angle_deg", From: "specs.open_angle_deg", Type: FieldInteger},
			{Name: "series_name", From: "series_name", Type: FieldText, Boost: 1},
		},
		BaseBoost: map[string]float64{fieldTitle: 4},
	}
	got := mapping.SearchFields()
	want := []string{
		"model^5", "series_name",
		fieldTitle + "^4", fieldSource + "^3", fieldHeadingPath + "^2",
		fieldMetadataText + "^2", fieldContent,
	}
	if diff := diffStrings(got, want); diff != "" {
		t.Errorf("打分字段表不符合预期: %s\n实际: %v", diff, got)
	}
}

// TestExactFieldsOnlyCoversDeclaredKeyword 钉住精确通道的字段来源。
//
// 只为声明了 keyword 的字段生成子句：对不存在的子字段做 term 查询，ES 不报错、
// 只是永远不命中，于是「精确匹配没生效」会伪装成「排序不够好」。
func TestExactFieldsOnlyCoversDeclaredKeyword(t *testing.T) {
	mapping := &Mapping{
		Fields: []FieldSpec{
			{Name: "model", From: "model", Type: FieldText, Boost: 5, Keyword: true},
			{Name: "product_name", From: "product_name", Type: FieldText, Boost: 4},
			{Name: "open_angle_deg", From: "specs.open_angle_deg", Type: FieldInteger},
			{Name: "zero_boost", From: "x", Type: FieldText, Keyword: true},
		},
	}
	got := mapping.ExactFields()
	if len(got) != 1 {
		t.Fatalf("精确字段是 %+v，应当只有 model 一条", got)
	}
	// 权重沿用字段自身的值：「精确比词元更值钱」由检索策略里那条独立通道的
	// 权重表达，这里再放大一遍等于把同一个判断算两次。
	if got[0].Name != "model.keyword" || got[0].Boost != 5 {
		t.Errorf("精确字段是 %+v，期望 model.keyword 权重 5", got[0])
	}
}

// TestFingerprintTracksKeywordSubfield 说明哪些改动会让已有文档变「陈旧」。
//
// keyword 子字段的有无会改变索引形态，必须进指纹；权重只影响查询期排序，不进。
func TestFingerprintTracksKeywordSubfield(t *testing.T) {
	mapping := &Mapping{
		Fields: []FieldSpec{
			{Name: "model", From: "model", Type: FieldText, Boost: 5, Keyword: true},
		},
	}
	withKeyword := mapping.fingerprint("cjk")

	mapping.Fields[0].Keyword = false
	withoutKeyword := mapping.fingerprint("cjk")

	if withKeyword == withoutKeyword {
		t.Fatal("keyword 子字段的有无会改变索引形态，指纹必须不同")
	}

	mapping.Fields[0].Keyword = true
	mapping.Fields[0].Boost = 99
	if mapping.fingerprint("cjk") != withKeyword {
		t.Error("权重不改变索引形态，指纹不该跟着变")
	}
}

// TestFlattenMetadata 守住兜底字段的摊平口径。
func TestFlattenMetadata(t *testing.T) {
	text := FlattenMetadata(map[string]any{
		"product_id": "H105P",
		"specs_from_doc": map[string]any{
			"door_material":  "木门",
			"open_angle_deg": 100,
			"surface_finish": "镀钛镍",
		},
		"variants": []any{"H105P-白", "H105P-黑"},
		"flag":     true,
		"empty":    "",
		"nil":      nil,
	})

	// 嵌套与数组里的值必须都进得来：规格明细几乎全在嵌套里，只处理顶层会让
	// 「按门板材质搜」这一类全灭。
	for _, want := range []string{"H105P", "木门", "100", "镀钛镍", "H105P-白", "H105P-黑"} {
		if !strings.Contains(text, want) {
			t.Errorf("摊平结果缺少 %q: %s", want, text)
		}
	}
	// 键不收、布尔不收：前者是英文标识符，后者命中「真/假」只会造噪声。
	for _, absent := range []string{"door_material", "open_angle_deg", "true"} {
		if strings.Contains(text, absent) {
			t.Errorf("摊平结果不该包含 %q: %s", absent, text)
		}
	}
}

// TestFlattenMetadataIsDeterministic 钉住「同一份元数据每次拍出同样的文本」。
//
// map 的遍历顺序在 Go 里是随机的；顺序不定的话，索引文档的 _source 会随进程
// 抖动，比对两次写入的差异、复现问题都会变得困难。
func TestFlattenMetadataIsDeterministic(t *testing.T) {
	metadata := map[string]any{
		"b": "第二", "a": "第一", "c": "第三",
		"specs": map[string]any{"z": "末", "y": "中", "x": "首"},
	}
	first := FlattenMetadata(metadata)
	for i := 0; i < 20; i++ {
		if got := FlattenMetadata(metadata); got != first {
			t.Fatalf("第 %d 次摊平结果不同:\n  %s\n  %s", i, first, got)
		}
	}
}

// TestFlattenMetadataTruncates 守住长度上限：不截断的话，某个键里塞一整段正文
// 会把 BM25 的长度归一化带偏，元数据写得越全的文档反而排到后面。
func TestFlattenMetadataTruncates(t *testing.T) {
	text := FlattenMetadata(map[string]any{"note": strings.Repeat("长", maxMetadataTextLen)})
	if len(text) > maxMetadataTextLen {
		t.Errorf("摊平结果长度是 %d，超过上限 %d", len(text), maxMetadataTextLen)
	}
	if text == "" {
		t.Error("截断不该把内容清空：前缀里的型号通常才是要搜的东西")
	}
}

// productMetadataFixture 是一份贴合解析器实际产出的产品元数据。
//
// 就用解析器那套键名（见 internal/rag/parser 的 productMetadata.toMap）：诊断
// 「兜底字段里有什么」必须基于真实形态，手编一份简化版会把问题一起简化掉。
func productMetadataFixture() map[string]any {
	return map[string]any{
		"product_id":    "GZ-H105P",
		"model":         "H105P",
		"family_prefix": "H105",
		"series_name":   "图冠系列",
		"product_name":  "固装铰链",
		"category_l1":   "铰链",
		"category_l2":   "固装铰链",
		"source_doc":    "图冠五金产品型录.md",
		"specs_from_doc": map[string]any{
			"mechanics_type":        "液压缓冲",
			"install_type":          "固装",
			"adjust_type":           "三维可调",
			"door_material":         "拉丝不锈钢",
			"base_material":         "冷轧钢",
			"surface_finish":        "镀钛镍",
			"open_angle_deg":        110,
			"cup_diameter_mm":       35,
			"door_thickness_min_mm": 16,
			"door_thickness_max_mm": 22,
		},
		"variants": []any{"H105P-A", "H105P-B"},
	}
}

// fallbackTokens 把兜底文本切成词元集合。
//
// 断言用词元相等而不是子串包含：兜底文本是把值按空格拼起来的，而值之间会互相
// 包含（GZ-H105P 里就有 H105P）。用子串判断会把「product_id 没成列、model 成列」
// 这种正常情况读成「model 重复进了兜底字段」—— 测试自己制造假警报，然后为了
// 让它闭嘴去改被测代码，是比没有测试更坏的结果。
func fallbackTokens(text string) map[string]struct{} {
	tokens := make(map[string]struct{})
	for _, token := range strings.Fields(text) {
		tokens[token] = struct{}{}
	}
	return tokens
}

// TestProjectExcludesPromotedValuesFromFallback 是本文件里最重要的一条：
// 已单独成列的值不许再进兜底字段。
//
// 两边都留一份的后果是排序失真，而不是「多占空间」。中文业务词（铰链 / 固装 /
// 系列名）几乎每篇文档都有，IDF 接近零，本来就不该左右排序；重复计入把它们在
// 一部分文档里的 tf 抬到别人的两三倍，于是「明明问的是固装铰链，结果全被拽到
// 只含铰链的文档上」。同一个值在同一篇文档里出现两次，不增加任何召回。
func TestProjectExcludesPromotedValuesFromFallback(t *testing.T) {
	mapping, err := LoadMapping(shippedMappingPath)
	if err != nil {
		t.Fatalf("加载随仓库发布的映射: %v", err)
	}

	columns, fallback := mapping.Project(productMetadataFixture())
	tokens := fallbackTokens(fallback)

	// 声明里成列的值：兜底字段里必须一个都不剩。
	promoted := []string{
		"GZ-H105P", "H105P", "H105", "图冠系列", "固装铰链", "铰链", "H105P-A", "H105P-B",
	}
	for _, value := range promoted {
		if _, ok := tokens[value]; ok {
			t.Errorf("已单独成列的值 %q 又进了兜底字段: %s", value, fallback)
		}
	}

	// 反过来的那一半同样重要：没成列的值一个都不能少，否则排除就成了静默丢内容。
	kept := []string{
		"液压缓冲", "固装", "三维可调", "拉丝不锈钢", "冷轧钢", "镀钛镍",
		"110", "35", "16", "22", "图冠五金产品型录.md",
	}
	for _, value := range kept {
		if _, ok := tokens[value]; !ok {
			t.Errorf("没成列的值 %q 不该从兜底字段消失: %s", value, fallback)
		}
	}

	// 成列的字段本身要拿到值：排除的前提是「确实取到了」，不是「声明里写了」。
	for _, name := range []string{"model", "product_id", "series_name", "category_l2", "variants"} {
		if _, ok := columns[name]; !ok {
			t.Errorf("字段 %s 应当取到值，否则它会被误排除出兜底字段", name)
		}
	}
}

// TestProjectKeepsUnresolvableValuesInFallback 守住排除的安全边界。
//
// 排除只针对**确实取到值**的路径。如果按「声明里写了 from 就排除」来做，
// 一个拼错的路径（或该文档恰好没有那个键）会让这个值从检索面整体消失 ——
// 那是「配错一处配置，内容悄悄搜不到」，比配错本身难查得多。
func TestProjectKeepsUnresolvableValuesInFallback(t *testing.T) {
	mapping := &Mapping{Fields: []FieldSpec{
		{Name: "model", From: "model", Type: FieldText, Boost: 5},
		// 路径拼错：元数据里叫 series_name，不会取到值。
		{Name: "series", From: "series_nam", Type: FieldText, Boost: 3},
		// 类型对不上：variants 是数组，没声明 multi 就取不到（toString 对切片返回 false）。
		{Name: "variants", From: "variants", Type: FieldText, Boost: 2},
	}}

	columns, fallback := mapping.Project(productMetadataFixture())

	if _, ok := columns["model"]; !ok {
		t.Fatal("取到值的字段应当在列里")
	}
	if _, ok := columns["series"]; ok {
		t.Error("路径拼错时不该取到值")
	}
	if _, ok := columns["variants"]; ok {
		t.Error("数组不声明 multi 时不该取到值")
	}

	tokens := fallbackTokens(fallback)
	// 这两条路径的内容都要留在兜底字段里。
	for _, value := range []string{"图冠系列", "H105P-A", "H105P-B"} {
		if _, ok := tokens[value]; !ok {
			t.Errorf("取不到值的路径不能连兜底也一起丢：%q 不在 %s", value, fallback)
		}
	}
	// 而确实成列的 model 不许重复出现；product_id 没声明，它的值照旧留着。
	if _, ok := tokens["H105P"]; ok {
		t.Errorf("成列的值不该再进兜底字段: %s", fallback)
	}
	if _, ok := tokens["GZ-H105P"]; !ok {
		t.Errorf("没声明成列的 product_id 应当留在兜底字段: %s", fallback)
	}
}

// TestProjectWithoutFieldsFlattensEverything 钉住「没配业务字段」这一档。
//
// 映射里一个字段都没声明时，兜底字段是元数据唯一的检索面；这时候还去排除的话，
// 整份元数据都搜不到。
func TestProjectWithoutFieldsFlattensEverything(t *testing.T) {
	mapping := &Mapping{}

	columns, fallback := mapping.Project(productMetadataFixture())
	if len(columns) != 0 {
		t.Errorf("没有声明字段时不该取到任何列: %v", columns)
	}
	for _, value := range []string{"H105P", "固装铰链", "图冠系列", "拉丝不锈钢"} {
		if !strings.Contains(fallback, value) {
			t.Errorf("没有业务字段时兜底字段必须兜住 %q: %s", value, fallback)
		}
	}
}

// TestProjectIsDeterministic 钉住 Project 与 FlattenMetadata 一样是确定的。
//
// 不确定的话索引文档的 _source 会随进程抖动，比对两次写入的差异、复现问题
// 都会变得困难。
func TestProjectIsDeterministic(t *testing.T) {
	mapping, err := LoadMapping(shippedMappingPath)
	if err != nil {
		t.Fatalf("加载随仓库发布的映射: %v", err)
	}

	_, first := mapping.Project(productMetadataFixture())
	for i := 0; i < 20; i++ {
		if _, got := mapping.Project(productMetadataFixture()); got != first {
			t.Fatalf("第 %d 次投影结果不同:\n  %s\n  %s", i, first, got)
		}
	}
}

// TestFingerprintTracksFallbackShape 钉住「改了兜底口径就必须提示 reindex」。
//
// 兜底字段的写入口径不是 mapping 声明的一部分，却一样决定索引里存了什么。
// 不算进指纹的话，改动之后只有新写入的文档是干净的，老文档继续带着旧的重复文本
// 参与排序 —— 表现是「改了半天没效果」，而没有任何地方提示要重建索引。
func TestFingerprintTracksFallbackShape(t *testing.T) {
	mapping, err := LoadMapping(shippedMappingPath)
	if err != nil {
		t.Fatalf("加载随仓库发布的映射: %v", err)
	}

	rev := mapping.fingerprint("ik_max_word")
	if rev == "" {
		t.Fatal("指纹不该为空")
	}

	// 同一份声明必须拍出同一个指纹，否则每次启动都会报「需要 reindex」。
	if again := mapping.fingerprint("ik_max_word"); again != rev {
		t.Errorf("同一份声明的指纹不稳定: %s != %s", rev, again)
	}

	// 兜底口径版本必须参与计算：改它等于改了索引内容，老文档就该被标为陈旧。
	if mapping.fingerprintWith("ik_max_word", fallbackShapeRev+"-test") == rev {
		t.Error("兜底字段的写入口径变了，指纹必须跟着变")
	}
}

// TestLookupPath 覆盖路径解析。
func TestLookupPath(t *testing.T) {
	metadata := map[string]any{
		"model":  "H105P",
		"specs":  map[string]any{"angle": 100},
		"scalar": "x",
	}
	cases := map[string]struct {
		path string
		want any
		ok   bool
	}{
		"顶层":    {path: "model", want: "H105P", ok: true},
		"嵌套":    {path: "specs.angle", want: 100, ok: true},
		"不存在":   {path: "brand", ok: false},
		"穿过标量":  {path: "scalar.inner", ok: false},
		"嵌套不存在": {path: "specs.size", ok: false},
		"空路径":   {path: "", ok: false},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := lookupPath(metadata, testCase.path)
			if ok != testCase.ok {
				t.Fatalf("命中状态是 %v，期望 %v", ok, testCase.ok)
			}
			if ok && got != testCase.want {
				t.Errorf("取到 %#v，期望 %#v", got, testCase.want)
			}
		})
	}
}

func writeMappingFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mapping.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入映射文件: %v", err)
	}
	return path
}

// equalValue 比较两个值，切片按元素比较。
func equalValue(got, want any) bool {
	gotSlice, gotIsSlice := got.([]any)
	wantSlice, wantIsSlice := want.([]any)
	if gotIsSlice != wantIsSlice {
		return false
	}
	if !gotIsSlice {
		return got == want
	}
	if len(gotSlice) != len(wantSlice) {
		return false
	}
	for index := range gotSlice {
		if gotSlice[index] != wantSlice[index] {
			return false
		}
	}
	return true
}

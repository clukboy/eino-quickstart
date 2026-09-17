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

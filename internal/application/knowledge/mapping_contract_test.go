package knowledge

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"eino-quickstart/internal/platform/storage/es"
	"eino-quickstart/internal/rag/parser"
)

// 这个文件守住「映射声明」与「parser 产出」之间的一致性。
//
// 两者是同一份契约的两端，但分处不同的包、由不同的改动驱动：parser 那边加一个
// 元数据键（比如 category_l3），映射这边不会报错，那个键会静静躺进兜底字段；
// 反过来映射里 from 写错一个字母，字段永远为空。两种情况的症状都是「按某些词搜
// 不到」，而不是任何异常。
//
// 所以这里用**真实的 ProductParser** 产出来核对，而不是手写一份元数据样例：
// 手写的样例只能证明「我以为 parser 会产出什么」。

// shippedMappingPath 指向真正发给运维的那份映射文件。
//
// 断言真文件而不是 testdata 里的副本：副本一定会漂移，漂移的方式恰好是测试
// 全绿、生产行为已经变了。
const shippedMappingPath = "../../../configs/es/chunk_mapping.yaml"

// catchAllKeys 是允许只进兜底字段（不单独成列）的 parser 键。
//
// 这是一份**显式清单**而不是「凡是没有声明的都放行」：parser 新增键时，这个测试
// 会失败并要求人来决定它怎么检索 —— 单独成列拿独立权重，还是进兜底字段。
// 默认放行的话，新键会悄悄混进兜底字段，功能上「能用」，但检索质量没人评估过。
var catchAllKeys = map[string]struct{}{
	// 规格明细：换产品线时字段会变（开合角度、杯径、门板厚度…），全部单独成列
	// 会让映射文件跟着每一类产品一起长。它们共享兜底字段的权重。
	//
	// 要把其中某一个提成独立字段，在 configs/es/chunk_mapping.yaml 里加一条下钻声明
	// 即可（`from: specs_from_doc.door_material`）—— 加了下钻声明之后，本清单里的
	// "specs_from_doc" 条目仍然可以留着：它表示「其余规格继续走兜底字段」，
	// 而 TestShippedMappingCoversEveryParserKey 按 from 的第一段判断覆盖，
	// 声明了任何一个 specs_from_doc.* 就算这一整块有归宿。
	//
	// 注意不要声明 `from: specs_from_doc`（整块）：那是嵌套对象，转不成字段值，
	// 会得到一个永远为空的字段，由 TestShippedFieldsResolveToScalars 拦住。
	"specs_from_doc": {},
	// 产品块来自哪个原始文件。同一份文件的每个产品块都一样，权重上相当于一个
	// 恒定的背景词，害处有限；而「按原始资料名找产品」是需要支持的用法。
	"source_doc": {},
}

// productBlock 是一份产品块，元数据取自线上的真实内容（H105P / 图冠系列）。
//
// 用它而不是编一份干净的样例：编造的样例会不自觉地把规格写成字符串，
// 正好绕过「YAML 里的 100 是 int、经 JSON 列读回来是 float64」这条分支。
const productBlock = `---
product_id: "H105P"
model: "H105P"
family_prefix: "H105"
series_name: "图冠系列"
product_name: "二段力小角度偏心轮快装缓冲铰链"
category_l1: "铰链"
category_l2: "缓冲铰链"
specs_from_doc:
  mechanics_type: "二段力"
  install_type: "快装"
  adjust_type: "偏心轮"
  door_material: "木门"
  base_material: "冷轧钢"
  surface_finish: "镀钛镍"
  open_angle_deg: 100
  cup_diameter_mm: 35
  door_thickness_min_mm: 15
  door_thickness_max_mm: 28
variants: []
source_doc: "图特知识库模版铰链-新(1)(1).docx"
---

## 安装说明

杯座安装孔距 35mm，门板厚度 15-28mm 适用。
`

// productBlockWithVariants 是多一个型号变体的同款产品，用来覆盖数组取值。
const productBlockWithVariants = `---
product_id: "H105P"
model: "H105P"
series_name: "图冠系列"
specs_from_doc:
  open_angle_deg: 100
variants:
  - "H105P-白"
  - "H105P-黑"
---

## 色号
`

// TestShippedMappingPathsResolveOnParserOutput 是防「from 路径写错」的那一道。
//
// 路径写错不会报错，那个字段只是永远是空的 —— 表现是「按型号搜不到」，而人会
// 先去怀疑分词器和 embedding。
func TestShippedMappingPathsResolveOnParserOutput(t *testing.T) {
	mapping := loadShippedMapping(t)
	for _, block := range []string{productBlock, productBlockWithVariants} {
		metadata := parseProductBlocks(t, block)[0]
		if missing := mapping.MissingPaths(metadata); len(missing) > 0 {
			t.Errorf(
				"映射里这些字段的 from 路径在 parser 产出里取不到值：\n  %s\n"+
					"元数据实际有的键：%s",
				strings.Join(missing, "\n  "), strings.Join(sortedKeys(metadata), ", "),
			)
		}
	}
}

// TestShippedMappingCoversEveryParserKey 是防「parser 新增了元数据键却没人决定
// 它怎么检索」的那一道。
//
// 失败时的处理不是改测试，而是做决定：单独成列（在 configs/es/chunk_mapping.yaml
// 里加一条）还是进兜底字段（加进本文件的 catchAllKeys 并说明理由）。
func TestShippedMappingCoversEveryParserKey(t *testing.T) {
	mapping := loadShippedMapping(t)

	declared := make(map[string]struct{}, len(mapping.Fields))
	for _, spec := range mapping.Fields {
		declared[topSegment(spec.From)] = struct{}{}
	}

	metadata := parseProductBlocks(t, productBlock)[0]
	for _, key := range sortedKeys(metadata) {
		// 下划线开头的是链路注入的溯源码（_source / _extension / _file_name），
		// 已经由共有字段承载，不属于业务检索面。
		if strings.HasPrefix(key, "_") {
			continue
		}
		if _, ok := declared[key]; ok {
			continue
		}
		if _, ok := catchAllKeys[key]; ok {
			continue
		}
		t.Errorf(
			"parser 产出的键 %q 没有归宿：映射里没有它的字段，也不在允许进兜底字段的清单里\n"+
				"  要单独成列：在 configs/es/chunk_mapping.yaml 的 fields 里加一条\n"+
				"  要进兜底字段：把它加进本文件的 catchAllKeys 并说明理由",
			key,
		)
	}

	// 反向：映射声明的字段，其 from 路径必须在 parser 产出里真实存在，否则那是一个
	// 永远为空的字段。用 resolvePath 而不是 metadata[from] —— 后者对
	// "specs_from_doc.door_material" 这种下钻路径永远查不到，会把「有值但这一步
	// 恰好为空」误报成路径缺失。
	for _, spec := range mapping.Fields {
		if _, ok := resolvePath(metadata, spec.From); !ok {
			t.Errorf("字段 %s 的 from=%s 在 parser 产出里不存在", spec.Name, spec.From)
		}
	}
}

// TestExtractKeepsProductIdentity 用真实 parser 输出钉住最终取到的值。
//
// 这几个字段是「按型号找资料」最直接的命中面，任一读空都意味着那一路权重
// （model^5）白给，而检索侧完全看不出差别。
func TestExtractKeepsProductIdentity(t *testing.T) {
	mapping := loadShippedMapping(t)
	metadata := parseProductBlocks(t, productBlock)[0]
	values := mapping.Extract(metadata)

	for field, want := range map[string]any{
		"product_id":    "H105P",
		"model":         "H105P",
		"family_prefix": "H105",
		"series_name":   "图冠系列",
		"product_name":  "二段力小角度偏心轮快装缓冲铰链",
		"category_l1":   "铰链",
		"category_l2":   "缓冲铰链",
	} {
		if values[field] != want {
			t.Errorf("字段 %s 的值是 %#v，期望 %#v", field, values[field], want)
		}
	}
	// 空数组的产品不该产出变体字段。
	if _, ok := values["variants"]; ok {
		t.Errorf("空的变体数组不该产出字段: %#v", values["variants"])
	}
}

// TestExtractKeepsVariants 覆盖「变体是数组」这条路径：只取到第一个元素会静默
// 截断，用户搜第二个色号就找不到。
func TestExtractKeepsVariants(t *testing.T) {
	mapping := loadShippedMapping(t)
	metadata := parseProductBlocks(t, productBlockWithVariants)[0]
	values := mapping.Extract(metadata)

	variants, ok := values["variants"].([]any)
	if !ok {
		t.Fatalf("变体没有被取成数组: %#v", values["variants"])
	}
	if len(variants) != 2 || variants[0] != "H105P-白" || variants[1] != "H105P-黑" {
		t.Errorf("变体取值错误: %#v", variants)
	}
}

// TestSpecsReachCatchAllThroughRealParser 守住「规格明细最终能被搜到」这条
// 端到端断言：parser 写入 → 机制键剔除 → 摊平进兜底字段。
func TestSpecsReachCatchAllThroughRealParser(t *testing.T) {
	metadata := parseProductBlocks(t, productBlock)[0]
	text := es.FlattenMetadata(searchableMetadata(metadata))

	for _, want := range []string{
		"二段力", "快装", "偏心轮", "木门", "冷轧钢", "镀钛镍",
		"100", "35", "15", "28", "图特知识库模版铰链-新(1)(1).docx",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("兜底字段里缺少 %q: %s", want, text)
		}
	}
	// 溯源码不该进兜底字段：_source 值就是文件名，已经有独立字段承载。
	if strings.Contains(text, "H105P.md") {
		t.Errorf("兜底字段里出现了溯源码: %s", text)
	}
}

// loadShippedMapping 加载发给运维的那份映射。
func loadShippedMapping(t *testing.T) *es.Mapping {
	t.Helper()
	mapping, err := es.LoadMapping(shippedMappingPath)
	if err != nil {
		t.Fatalf("加载映射 %s: %v", shippedMappingPath, err)
	}
	return mapping
}

// parseProductBlocks 用真实的 ProductParser 解析产品块，返回每个块的元数据。
func parseProductBlocks(t *testing.T, content string) []map[string]any {
	t.Helper()
	docs, err := parser.ProductParser{}.Parse(context.Background(), strings.NewReader(content))
	if err != nil {
		t.Fatalf("解析产品块: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("没有解析出任何产品块")
	}
	out := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		out = append(out, doc.MetaData)
	}
	return out
}

// topSegment 取取值路径的第一段，用来判断一个 parser 键有没有被声明覆盖。
func topSegment(path string) string {
	if index := strings.Index(path, "."); index >= 0 {
		return path[:index]
	}
	return path
}

// TestShippedFieldsResolveToScalars 防住一类比「路径写错」更隐蔽的配置错误：
// 路径存在，但取到的是容器而不是标量，于是字段被建出来却永远是空的。
//
// 最典型的写法是把整个 specs_from_doc 当成一个字段：
//
//   - name: specs_from_doc
//     from: specs_from_doc
//     type: text
//
// `specs_from_doc` 在 parser 产出里是嵌套 map，既不参与分词也没法强转成 text，
// Extract 会静默跳过它 —— 于是这个字段永远为空，症状还是「按规格搜不到」。
// MissingPaths 也放行它：路径确实存在，只是值不是标量。这一类只能靠形状核对拦住。
func TestShippedFieldsResolveToScalars(t *testing.T) {
	mapping := loadShippedMapping(t)
	metadata := parseProductBlocks(t, productBlock)[0]
	if problems := containerFieldProblems(mapping, metadata); len(problems) > 0 {
		t.Errorf(
			"映射里这些字段的取值路径与元数据形状不匹配：\n  %s",
			strings.Join(problems, "\n  "),
		)
	}
}

// TestContainerFieldGuardIsNotVacuous 用构造出来的坏声明证明上面那道守卫不是空的。
//
// 没有这一条，守卫在「shipped 映射刚好没有这类错误」时会一直绿着，谁也不知道它
// 到底会不会拦人。这里同时钉住 Extract 对容器值的实际行为，让「静默为空」这件事
// 有据可查。
func TestContainerFieldGuardIsNotVacuous(t *testing.T) {
	metadata := parseProductBlocks(t, productBlock)[0]

	wholeMap := &es.Mapping{Fields: []es.FieldSpec{
		{Name: "specs_from_doc", From: "specs_from_doc", Type: es.FieldText, Boost: 3},
	}}
	if problems := containerFieldProblems(wholeMap, metadata); len(problems) == 0 {
		t.Fatal("把整个 specs_from_doc 声明成一个字段时守卫没报错，说明它是空的")
	}
	if values := wholeMap.Extract(metadata); len(values) != 0 {
		t.Errorf("容器值不该产出字段值，实际: %#v", values)
	}
	if missing := wholeMap.MissingPaths(metadata); len(missing) != 0 {
		t.Errorf("MissingPaths 不负责这类错误，不该报路径缺失: %#v", missing)
	}
	// 带 multi 也救不了：map 不是数组，包成单元素数组后照样转不成 text。
	wholeMapMulti := &es.Mapping{Fields: []es.FieldSpec{
		{Name: "specs_from_doc", From: "specs_from_doc", Type: es.FieldText, Multi: true},
	}}
	if values := wholeMapMulti.Extract(metadata); len(values) != 0 {
		t.Errorf("给容器值加 multi 也不该产出字段值，实际: %#v", values)
	}

	// 漏写 multi 的数组字段：只会取到第一个元素，同样不报错。
	missingMulti := &es.Mapping{Fields: []es.FieldSpec{
		{Name: "variants", From: "variants", Type: es.FieldText},
	}}
	variantMetadata := parseProductBlocks(t, productBlockWithVariants)[0]
	if problems := containerFieldProblems(missingMulti, variantMetadata); len(problems) == 0 {
		t.Fatal("数组字段漏写 multi 时守卫没报错")
	}
}

// containerFieldProblems 报告「声明与元数据形状不匹配」的字段。
//
// 空值（这个产品没有该规格）不算问题：那是业务事实，不是配置错误 —— 与
// MissingPaths 只认路径是否存在是同一个取舍。
func containerFieldProblems(mapping *es.Mapping, metadata map[string]any) []string {
	var problems []string
	for _, spec := range mapping.Fields {
		raw, ok := resolvePath(metadata, spec.From)
		if !ok {
			continue // 路径不存在由 MissingPaths 负责。
		}
		value := derefValue(raw)
		if value == nil {
			continue
		}
		switch kind := reflect.ValueOf(value).Kind(); {
		case kind == reflect.Map:
			problems = append(problems, fmt.Sprintf(
				"%s <- %s：取到的是嵌套对象而不是标量，转不成字段值（这个字段永远是空的）；"+
					"要单独成列请下钻到具体键，比如 %s.<规格名>",
				spec.Name, spec.From, spec.From,
			))
		case (kind == reflect.Slice || kind == reflect.Array) && !spec.Multi:
			problems = append(problems, fmt.Sprintf(
				"%s <- %s：取到的是数组但没有声明 multi: true，只会取到第一个元素且不报错",
				spec.Name, spec.From,
			))
		}
	}
	sort.Strings(problems)
	return problems
}

// resolvePath 复刻 es 包 lookupPath 的语义：只认 map 逐层下降，不认数组下标。
func resolvePath(metadata map[string]any, path string) (any, bool) {
	var current any = metadata
	for _, segment := range strings.Split(path, ".") {
		node, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = node[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// derefValue 逐层解开指针与接口，解出空指针时返回 nil。
//
// parser 的数值规格声明成 *int，缺值时是 (*int)(nil)：不 deref 的话它会以 Ptr
// 的形态出现在分类里，被误判成「形状不对」。
func derefValue(value any) any {
	if value == nil {
		return nil
	}
	reflected := reflect.ValueOf(value)
	for reflected.Kind() == reflect.Pointer || reflected.Kind() == reflect.Interface {
		if reflected.IsNil() {
			return nil
		}
		reflected = reflected.Elem()
	}
	if !reflected.IsValid() {
		return nil
	}
	return reflected.Interface()
}

// sortedKeys 返回 map 的键，升序，让失败信息可复现。
func sortedKeys(metadata map[string]any) []string {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

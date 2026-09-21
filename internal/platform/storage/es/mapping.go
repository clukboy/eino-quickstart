package es

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/elastic/go-elasticsearch/v9/typedapi/types"
	"github.com/elastic/go-elasticsearch/v9/typedapi/types/enums/dynamicmapping"
	"gopkg.in/yaml.v3"
)

type FieldType string

const (
	FieldText    FieldType = "text"
	FieldKeyword FieldType = "keyword"
	FieldInteger FieldType = "integer"
	FieldLong    FieldType = "long"
	FieldDouble  FieldType = "double"
	FieldFloat   FieldType = "float"
	FieldBoolean FieldType = "boolean"
	FieldDate    FieldType = "date"
)

// knownFieldTypes 是给报错信息用的清单，顺序与上面一致。
const knownFieldTypes = "text / keyword / integer / long / double / float / boolean / date"

func parseFieldType(raw string) (FieldType, error) {
	switch FieldType(raw) {
	case FieldText, FieldKeyword, FieldInteger, FieldLong, FieldDouble, FieldFloat, FieldBoolean, FieldDate:
		return FieldType(raw), nil
	}
	return "", fmt.Errorf("未知字段类型 %q（可用：%s）", raw, knownFieldTypes)
}

const maxKeywordLength = 512

const mappingRevLength = 12

var indexDynamic = dynamicmapping.False

type baseFieldSpec struct {
	name    string
	kind    FieldType
	keyword bool
	boost   float64
}

// baseFields 是每个索引共有的字段。
//
// 它们的值由写入端从 ent 实体直接填（见 ChunkDoc），不从元数据里取：元数据是
// 入库那一刻的快照，文档改名、改可见性之后就不再正确，而检索侧要拿这些值做
// 引用与权限判断。
var baseFields = []baseFieldSpec{
	{name: fieldChunkID, kind: FieldKeyword},
	{name: fieldDocumentID, kind: FieldKeyword},
	{name: fieldDatasetID, kind: FieldKeyword},
	{name: fieldChunkIndex, kind: FieldInteger},

	// source 是回指原文的键，不能被分词器改写，必须留一份原样的 keyword：
	// 命中之后按它去 documents.source 取行。
	{name: fieldSource, kind: FieldText, keyword: true, boost: 3},
	// 标题与标题路径既参与打分，也用于按精确路径过滤/展示。
	{name: fieldTitle, kind: FieldText, keyword: true, boost: 3},
	{name: fieldHeadingPath, kind: FieldText, keyword: true, boost: 2},

	{name: fieldContent, kind: FieldText, boost: 1},

	{name: fieldMetadataText, kind: FieldText, boost: 2},

	// 这两个字段只写不查（见包注释：ACL 一律以 PostgreSQL 现值为准），留在索引
	// 里是为了排障时能看出「写入那一刻的归属是什么」。
	{name: fieldVisibility, kind: FieldKeyword},
	{name: fieldOwner, kind: FieldKeyword},
	{name: fieldIndexedAt, kind: FieldDate},

	// 形态指纹：只用于识别「哪些文档是更早的映射写下的」，不参与打分。
	{name: fieldMappingRev, kind: FieldKeyword},
}

// baseFieldNames 返回共有字段名集合，用于校验业务字段没有和它们重名。
func baseFieldNames() map[string]struct{} {
	names := make(map[string]struct{}, len(baseFields))
	for _, field := range baseFields {
		names[field.name] = struct{}{}
	}
	return names
}

// baseScoredBoosts 返回共有字段的默认权重（可被 Mapping.BaseBoost 覆盖）。
func baseScoredBoosts() map[string]float64 {
	boosts := make(map[string]float64, len(baseFields))
	for _, field := range baseFields {
		if field.boost > 0 {
			boosts[field.name] = field.boost
		}
	}
	return boosts
}

// Mapping 是一个索引的检索面声明，从配置文件加载。
//
// 它只描述「业务字段」。共有字段、动态映射策略这些所有索引都一样的部分留在
// 代码里（见 baseFields / indexDynamic）—— 每个部署都抄一遍它们，只会给
// 「配置写错了」多制造几个机会。
type Mapping struct {
	// Name 只用于日志与报错，方便确认「这个索引现在用的是哪份映射」。
	Name string `json:"name,omitempty"`

	// Fields 是业务字段：从分块元数据取值、单独成列的检索面。
	Fields []FieldSpec `json:"fields,omitempty"`

	// BaseBoost 覆盖共有字段的默认权重，键必须是可打分的共有字段名。
	//
	// 它不影响索引形态（权重是查询期的），所以改它不需要重建索引，也不会计入
	// 形态指纹。
	BaseBoost map[string]float64 `json:"baseBoost,omitempty"`
}

// FieldSpec 声明的是一条业务字段的完整契约。
//
// 一条声明同时决定三件事：写进索引的字段类型与形态（mapping）、它的值从元数据
// 的哪里来（写入）、以及它参不参与打分（查询）。三件事分开配置的话，「字段建了
// 但没写值」「写了值但没参与打分」这类错配不会有任何提示。
type FieldSpec struct {
	// Name 是索引里的字段名。
	Name string `json:"name"`

	// From 是分块元数据里的取值路径，如 "model" 或
	// "specs_from_doc.open_angle_deg"。路径写错不会报错，只会让这个字段永远
	// 是空的 —— 所以 LoadMapping 之后有测试拿真实 parser 输出逐个路径验证。
	From string `json:"from"`

	Type FieldType `json:"type"`

	// Boost 大于 0 时该字段进入 multi_match 的字段表，值为权重。
	//
	// 权重回答的是「同一个词出现在哪里，更能说明这就是用户要的那一篇」。型号
	// 这类业务主键式的值通常只出现在元数据里、正文里一次都不出现，所以它们
	// 普遍拿比正文高得多的权重。
	Boost float64 `json:"boost,omitempty"`

	// Analyzer 覆盖索引默认分词器（只对 text 有意义）。
	Analyzer string `json:"analyzer,omitempty"`

	// Keyword 在 text 字段上再挂一份 keyword 子字段，供精确过滤/原样取回。
	Keyword bool `json:"keyword,omitempty"`

	// Multi 表示这个字段的值是数组（如型号变体 variants）。
	//
	// 必须显式声明：单个值写进数组字段也能索引，但反过来「本来该是数组的值
	// 被当成单值」会只取到第一个 —— 这种截断不会报错。
	Multi bool `json:"multi,omitempty"`
}

// LoadMapping 从 JSON 或 YAML 文件加载映射。
//
// 两种格式都收是为了迁就两边的既有习惯：ES 的 mapping 原生是 JSON，能直接和
// 集群里的定义对照；而产品型录的元数据源文件是 YAML，配置写在一起时少一次
// 心智切换。解析结果完全一致。
func LoadMapping(path string) (*Mapping, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("es: 读取映射文件 %q: %w", path, err)
	}
	body, err := normalizeMappingBytes(path, raw)
	if err != nil {
		return nil, err
	}

	mapping := &Mapping{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	// 拒绝未知字段：多打一个 boostt 不会报错的话，那个字段就变成「建了列、
	// 没有值、也不参与打分」，而配置作者会以为自己配对了。
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(mapping); err != nil {
		return nil, fmt.Errorf("es: 解析映射文件 %q: %w", path, err)
	}
	if err := mapping.Validate(); err != nil {
		return nil, fmt.Errorf("es: 映射文件 %q 不可用: %w", path, err)
	}
	return mapping, nil
}

// normalizeMappingBytes 把非 JSON 的映射文件转成 JSON。
//
// yaml.v3 解映射得到的就是 map[string]any，可以直接被 encoding/json 序列化，
// 所以这里不需要递归转换键类型。
func normalizeMappingBytes(path string, raw []byte) ([]byte, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		var generic any
		if err := yaml.Unmarshal(raw, &generic); err != nil {
			return nil, fmt.Errorf("es: 解析映射文件 %q: %w", path, err)
		}
		body, err := json.Marshal(generic)
		if err != nil {
			return nil, fmt.Errorf("es: 转换映射文件 %q: %w", path, err)
		}
		return body, nil
	case ".json", "":
		return raw, nil
	default:
		return nil, fmt.Errorf("es: 映射文件 %q 的扩展名必须是 .json / .yaml / .yml", path)
	}
}

// Validate 检查映射声明是否自洽。
//
// 检查的都是「ES 会接受、但结果不是你想要的」那一类：字段名带点会静默变成嵌套
// 对象而不是字段，非 text 字段给 boost 会让那一路权重失效，与共有字段重名则会
// 出现两个同名声明互相覆盖。这些都不会在写入时报错。
func (m *Mapping) Validate() error {
	reserved := baseFieldNames()
	seen := make(map[string]struct{}, len(m.Fields))

	for index, spec := range m.Fields {
		where := fmt.Sprintf("fields[%d]", index)
		if spec.Name == "" {
			return fmt.Errorf("%s 缺少 name", where)
		}
		if spec.From == "" {
			return fmt.Errorf("%s（字段 %s）缺少 from：没有它这个字段永远是空的", where, spec.Name)
		}
		if err := validateFieldName(spec.Name); err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if _, clash := reserved[spec.Name]; clash {
			return fmt.Errorf(
				"字段名 %q 与共有字段重名；共有字段由代码提供，要调它的权重请用 baseBoost",
				spec.Name,
			)
		}
		if _, duplicate := seen[spec.Name]; duplicate {
			return fmt.Errorf("字段名 %q 重复声明", spec.Name)
		}
		seen[spec.Name] = struct{}{}

		if _, err := parseFieldType(string(spec.Type)); err != nil {
			return fmt.Errorf("%s（字段 %s）: %w", where, spec.Name, err)
		}
		if spec.Boost < 0 {
			return fmt.Errorf("%s（字段 %s）boost 不能为负", where, spec.Name)
		}
		if spec.Boost > 0 && !spec.scorable() {
			return fmt.Errorf(
				"%s（字段 %s）的类型是 %s，不能参与 BM25 打分；"+
					"boost 只对 text / keyword 有意义，其余类型请去掉 boost 供过滤或排序使用",
				where, spec.Name, spec.Type,
			)
		}
		if spec.Keyword && spec.Type != FieldText {
			return fmt.Errorf(
				"%s（字段 %s）只有 text 能挂 keyword 子字段；%s 本身就是精确匹配的",
				where, spec.Name, spec.Type,
			)
		}
		if spec.Analyzer != "" && spec.Type != FieldText {
			return fmt.Errorf(
				"%s（字段 %s）只有 text 能指定 analyzer；%s 不经过分词",
				where, spec.Name, spec.Type,
			)
		}
		if spec.Multi && spec.Type != FieldText && spec.Type != FieldKeyword {
			return fmt.Errorf(
				"%s（字段 %s）只有 text / keyword 能声明 multi；%s 是单值类型",
				where, spec.Name, spec.Type,
			)
		}
	}

	scored := baseScoredBoosts()
	for name := range m.BaseBoost {
		if _, ok := scored[name]; !ok {
			return fmt.Errorf(
				"baseBoost 里的 %q 不是可打分的共有字段；可用的是 %s",
				name, strings.Join(sortedNames(scored), " / "),
			)
		}
	}
	return nil
}

// validateFieldName 限制字段名字符集。
//
// 点号必须拒绝：ES 里 "a.b" 会建出一个 a 对象下的 b 字段，而写入端是按平铺的
// 键写值的，最后得到的是「字段存在但没人往里写」和「值在别处」两个都不报错的
// 结果。大写与连字符倒不致命，但混用只会让查询时打错。
func validateFieldName(name string) error {
	for index, char := range name {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= '0' && char <= '9' && index > 0:
		case char == '_' && index > 0:
		default:
			return fmt.Errorf(
				"字段名 %q 不合法：只允许小写字母、数字与下划线，且以字母开头（不能含点号）",
				name,
			)
		}
	}
	return nil
}

// scorable 报告一个字段能否进入 multi_match 的字段表。
//
// keyword 也算：它不经过分词，等于「整串精确匹配」—— 对型号这种既是标识符、
// 又经常被完整输入的词，精确匹配往往是更可贵的信号。
func (s FieldSpec) scorable() bool {
	return s.Type == FieldText || s.Type == FieldKeyword
}

// propertyFor 按声明造一个 ES 属性。
//
// 共有字段与业务字段共用这一个构造器：同一份声明在「建索引」和「补写 mapping」
// 两处必须造出完全一样的属性，否则会出现「新建的索引对、补写的索引不对」。
func propertyFor(kind FieldType, keyword bool, analyzer string) (types.Property, error) {
	switch kind {
	case FieldText:
		property := types.TextProperty{}
		// 不写 analyzer 就会落到索引默认分词器（standard），中文被切成单字，
		// BM25 立刻失去区分度。
		if analyzer != "" {
			property.Analyzer = &analyzer
		}
		if keyword {
			property.Fields = map[string]types.Property{
				keywordSubfield: types.KeywordProperty{IgnoreAbove: intPtr(maxKeywordLength)},
			}
		}
		return property, nil
	case FieldKeyword:
		return types.KeywordProperty{}, nil
	case FieldInteger:
		return types.IntegerNumberProperty{}, nil
	case FieldLong:
		return types.LongNumberProperty{}, nil
	case FieldDouble:
		return types.DoubleNumberProperty{}, nil
	case FieldFloat:
		return types.FloatNumberProperty{}, nil
	case FieldBoolean:
		return types.BooleanProperty{}, nil
	case FieldDate:
		return types.DateProperty{}, nil
	default:
		return nil, fmt.Errorf("未知字段类型 %q", kind)
	}
}

// properties 造完整的属性表：共有字段 + 业务字段。
//
// defaultAnalyzer 是索引级默认分词器（来自 es.analyzer），业务字段可以用自己的
// analyzer 覆盖 —— 一个索引里不同字段走不同分词器是 ES 的正常用法。
func (m *Mapping) properties(defaultAnalyzer string) (map[string]types.Property, error) {
	properties := make(map[string]types.Property, len(baseFields)+len(m.Fields))
	for _, field := range baseFields {
		property, err := propertyFor(field.kind, field.keyword, defaultAnalyzer)
		if err != nil {
			return nil, fmt.Errorf("es: 共有字段 %s: %w", field.name, err)
		}
		properties[field.name] = property
	}
	for _, spec := range m.Fields {
		analyzer := spec.Analyzer
		if analyzer == "" {
			analyzer = defaultAnalyzer
		}
		property, err := propertyFor(spec.Type, spec.Keyword, analyzer)
		if err != nil {
			return nil, fmt.Errorf("es: 字段 %s: %w", spec.Name, err)
		}
		properties[spec.Name] = property
	}
	return properties, nil
}

// SearchFields 是 multi_match 的字段权重表。
//
// 顺序是「先业务字段（按声明顺序）、后共有字段（按 baseFields 顺序）」：业务
// 字段是配置作者自己排的优先级，保持原样最符合预期；共有字段的顺序固定。
// ES 不在乎顺序，这里的顺序是为了让人读 DSL 时能直接看出优先级。
func (m *Mapping) SearchFields() []string {
	fields := make([]string, 0, len(m.Fields)+len(baseFields))
	for _, spec := range m.Fields {
		if spec.Boost > 0 {
			fields = append(fields, weightedField(spec.Name, spec.Boost))
		}
	}
	boosts := baseScoredBoosts()
	for name, boost := range m.BaseBoost {
		boosts[name] = boost
	}
	// 共有字段按权重降序排，而不是声明顺序：声明表是按「字段是什么」分组的
	// （ID 一组、文本一组、ACL 一组），照它排会让 DSL 读起来毫无优先级线索。
	scored := make([]baseFieldSpec, 0, len(baseFields))
	for _, field := range baseFields {
		if boost, ok := boosts[field.name]; ok && field.boost > 0 {
			scored = append(scored, baseFieldSpec{name: field.name, boost: boost})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].boost > scored[j].boost })
	for _, field := range scored {
		fields = append(fields, weightedField(field.name, field.boost))
	}
	return fields
}

// weightedField 拼出 multi_match 的 "字段^权重"。
//
// 权重 1 不加后缀（等价于 ^1）：让最常见的默认值不出现，DSL 更好读。
func weightedField(name string, boost float64) string {
	if boost == 1 {
		return name
	}
	return name + "^" + strconv.FormatFloat(boost, 'f', -1, 64)
}

// keywordSubfield 是 text 字段上 .keyword 子字段的名字（见 propertyFor）。
//
// 它存的是**原样的整值**，所以 term 查询它只有在「查询串与字段值逐字相等」时
// 才命中 —— 这正是 ExactFields 想要的性质。
const keywordSubfield = "keyword"

// ExactField 是参与逐字精确匹配的一个字段。
type ExactField struct {
	// Name 是完整的 ES 字段名（已带 .keyword 后缀）。
	Name string
	// Boost 沿用字段自身的权重。
	//
	// 「精确命中比词元命中更值钱」这件事不在这里表达，而是由检索策略里独立的一条
	// 通道权重表达（配置的 retrieval.exactWeight）。两处都放大等于把同一个判断
	// 算两遍，调的时候没人说得清最后是几倍。
	Boost float64
}

// ExactFields 返回参与逐字精确匹配的字段。
//
// 只包含声明了 keyword 的字段。没声明的字段不能出现在这里：对不存在的子字段做
// term 查询，ES 不报错、只是永远不命中，于是「精确匹配没生效」会表现为「排序
// 不够好」，没人能看出来。
func (m *Mapping) ExactFields() []ExactField {
	fields := make([]ExactField, 0, len(m.Fields))
	for _, spec := range m.Fields {
		if !spec.Keyword || spec.Boost <= 0 {
			continue
		}
		fields = append(fields, ExactField{
			Name:  spec.Name + "." + keywordSubfield,
			Boost: spec.Boost,
		})
	}
	return fields
}

// Extract 按字段声明从元数据里取值。
//
// 取不到、取到空值、取到的值转不成声明的类型，一律跳过这个字段：索引里缺一个
// 字段不是错误，而写入一个空串虽然不影响打分（空串分词后没有词元），却会白占
// 空间，也让「这个字段到底有没有值」变得看不出来。
//
// 元数据里已经剔除了链路机制键（由调用方在做，见 application/knowledge 的
// searchableMetadata），所以 from 只能指向业务键。
func (m *Mapping) Extract(metadata map[string]any) map[string]any {
	if len(m.Fields) == 0 || len(metadata) == 0 {
		return nil
	}
	values := make(map[string]any, len(m.Fields))
	for _, spec := range m.Fields {
		raw, ok := lookupPath(metadata, spec.From)
		if !ok {
			continue
		}
		value, ok := coerce(raw, spec)
		if !ok {
			continue
		}
		values[spec.Name] = value
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

// MissingPaths 报告哪些字段声明在给定元数据里取不到值（路径不存在）。
//
// 它服务于一类只有显式核对才能发现的错误：from 路径拼错时不会报错，那个字段
// 只是永远是空的，表现为「按型号搜不到」而不是任何异常。用于映射与 parser 产出
// 对齐的检查（见 mapping_contract_test.go）。
//
// 只判断路径是否存在，不判断值是否为空：产品 A 有这个规格、产品 B 没有，是业务
// 事实而不是配置错误。
func (m *Mapping) MissingPaths(metadata map[string]any) []string {
	var missing []string
	for _, spec := range m.Fields {
		if _, ok := lookupPath(metadata, spec.From); !ok {
			missing = append(missing, spec.Name+" <- "+spec.From)
		}
	}
	sort.Strings(missing)
	return missing
}

// fallbackShapeRev 是兜底字段（metadata_text）的写入口径版本。
//
// 它不是 mapping 声明的一部分，却一样决定「索引里到底存了什么」：改变了哪些值
// 会被收进兜底字段，已有文档的内容就与新口径不一致了。把它算进指纹，是为了让
// 这种改动也被预检当成「需要 reindex」报出来 —— 否则改完只有新文档是干净的，
// 老文档继续带着旧的重复文本参与排序，表现是「改了半天没效果」，而没有任何
// 地方提示要重建索引。
//
// 改动这个字段的规则：只要 Project 的产出对同一份元数据可能不同，就 +1。
const fallbackShapeRev = "v2-no-promoted-dupes"

// fingerprint 是映射在索引期的形态指纹。
//
// 它取代了手写的「形态版本号」：映射从代码搬到配置文件之后，那个版本号就成了
// 第二处需要人工同步的地方 —— 改了配置忘了 +1 的后果是，升级之后按型号搜不到
// 东西，而预检不会提示需要 reindex。指纹由声明本身算出来，改了就变。
//
// 指纹只覆盖**索引期**的形态：分词器、动态映射策略、字段名/类型/取值路径/
// 是否挂 keyword 子字段、兜底字段的写入口径。权重被排除在外 —— 它是查询期的东西
// （multi_match 的 boost），改权重不需要重建索引，把它算进去会让每次调权重都
// 触发一次全量 reindex 的假警报。
func (m *Mapping) fingerprint(analyzer string) string {
	return m.fingerprintWith(analyzer, fallbackShapeRev)
}

// fingerprintWith 是 fingerprint 的本体，兜底口径作为参数传入。
//
// 拆成两层是为了让「口径版本确实参与计算」这件事可被断言：常量没法在测试里改，
// 只能把它变成一个入参再喂两个不同的值。少了这条断言，「指纹没把兜底口径算进去」
// 这种漏改不会有任何表现，直到某次改了兜底字段却发现老文档没被提示重建。
func (m *Mapping) fingerprintWith(analyzer, fallbackRev string) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "analyzer=%s\ndynamic=%s\nfallback=%s\n",
		analyzer, indexDynamic.String(), fallbackRev)
	for _, field := range baseFields {
		fmt.Fprintf(hash, "base:%s=%s:keyword=%t\n", field.name, field.kind, field.keyword)
	}

	names := make([]string, 0, len(m.Fields))
	for _, spec := range m.Fields {
		names = append(names, spec.Name)
	}
	sort.Strings(names)
	byName := make(map[string]FieldSpec, len(m.Fields))
	for _, spec := range m.Fields {
		byName[spec.Name] = spec
	}
	for _, name := range names {
		spec := byName[name]
		fmt.Fprintf(hash, "field:%s=%s:from=%s:keyword=%t:multi=%t:analyzer=%s\n",
			spec.Name, spec.Type, spec.From, spec.Keyword, spec.Multi, spec.Analyzer)
	}
	return hex.EncodeToString(hash.Sum(nil))[:mappingRevLength]
}

// lookupPath 按 "specs_from_doc.open_angle_deg" 这样的路径取值。
//
// 只认 map 逐层下降：元数据经过 JSON 列存取，数组下标这类路径既不稳定也没有
// 实际需求，不支持反而少一类静默取错值的可能。
func lookupPath(metadata map[string]any, path string) (any, bool) {
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

// coerce 把元数据里的原始值转成字段声明的类型。
//
// 原始值的类型比声明的宽：同一份 YAML 解出来的可能是 string、int、*int，经过
// PostgreSQL 的 JSON 列再读回来又变成 float64。值取不到只是少一个字段，类型
// 转换失败却会被当成「字段没配」，所以这里对所有数值形态都放开。
func coerce(raw any, spec FieldSpec) (any, bool) {
	if spec.Multi {
		items := sliceOf(raw)
		values := make([]any, 0, len(items))
		for _, item := range items {
			if value, ok := coerceScalar(item, spec.Type); ok {
				values = append(values, value)
			}
		}
		if len(values) == 0 {
			return nil, false
		}
		return values, true
	}
	return coerceScalar(raw, spec.Type)
}

// sliceOf 把一个值规整成切片。
//
// 单值也包成单元素切片：YAML 里只有一个变体时解出来是 string 而不是数组，
// 直接断言会静默丢掉这个变体。
func sliceOf(raw any) []any {
	raw = deref(raw)
	if raw == nil {
		return nil
	}
	if items, ok := asSlice(raw); ok {
		return items
	}
	return []any{raw}
}

// asSlice 在值确实是切片或数组时展开它。
//
// 按 reflect.Kind 判断而不是只匹配 []any：同一份元数据可能是 []any（JSON 列
// 读回来的形态）也可能是 []string（代码里直接构造的形态），只认前者会让后者
// 的值静默消失 —— 表现是「某个变体搜不到」。
func asSlice(value any) ([]any, bool) {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Slice, reflect.Array:
		items := make([]any, 0, reflected.Len())
		for index := 0; index < reflected.Len(); index++ {
			items = append(items, reflected.Index(index).Interface())
		}
		return items, true
	default:
		return nil, false
	}
}

func coerceScalar(raw any, kind FieldType) (any, bool) {
	value := deref(raw)
	if value == nil {
		return nil, false
	}
	switch kind {
	case FieldText, FieldKeyword, FieldDate:
		text, ok := toString(value)
		if !ok || text == "" {
			return nil, false
		}
		return text, true
	case FieldInteger, FieldLong:
		number, ok := toInt64(value)
		if !ok {
			return nil, false
		}
		return number, true
	case FieldDouble, FieldFloat:
		number, ok := toFloat64(value)
		if !ok {
			return nil, false
		}
		return number, true
	case FieldBoolean:
		flag, ok := toBool(value)
		if !ok {
			return nil, false
		}
		return flag, true
	}
	return nil, false
}

// deref 逐层解引用指针与接口，解出空指针时返回 nil。
//
// 元数据里确实会出现指针：产品 parser 的规格字段声明成 *int，缺值时就是
// (*int)(nil)，而 fmt.Sprint 会把 nil 指针打成 "<nil>" 写进索引。
func deref(value any) any {
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

// toString 读文本。
//
// 布尔值当文本只会造噪声（搜「true」命中一堆文档），直接丢弃；数字放开，
// 因为「开合角度 110」这类值写在 text 字段里是合理的需求。
func toString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed), true
	case []byte:
		return strings.TrimSpace(string(typed)), true
	case bool:
		return "", false
	case fmt.Stringer:
		return strings.TrimSpace(typed.String()), true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.String:
		return strings.TrimSpace(reflected.String()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(reflected.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(reflected.Uint(), 10), true
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(reflected.Float(), 'f', -1, 64), true
	}
	return "", false
}

func toInt64(value any) (int64, bool) {
	if text, ok := stringKind(value); ok {
		number, err := strconv.ParseInt(text, 10, 64)
		if err == nil {
			return number, true
		}
		// "100.0" 这类小数形态也收：YAML 里 100.0 与 100 会被解成同一个
		// 数字，但写成字符串时形态不固定。
		asFloat, floatErr := strconv.ParseFloat(text, 64)
		if floatErr != nil {
			return 0, false
		}
		return int64(asFloat), true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflected.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(reflected.Uint()), true
	case reflect.Float32, reflect.Float64:
		return int64(reflected.Float()), true
	}
	return 0, false
}

func toFloat64(value any) (float64, bool) {
	if text, ok := stringKind(value); ok {
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		return number, true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(reflected.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(reflected.Uint()), true
	case reflect.Float32, reflect.Float64:
		return reflected.Float(), true
	}
	return 0, false
}

// toBool 读布尔。
//
// 收 "true" / "1" 这类字符串形态：元数据里的布尔经常是从文本表单或 JSON 里
// 带过来的，形态不固定。
func toBool(value any) (bool, bool) {
	if flag, ok := value.(bool); ok {
		return flag, true
	}
	if text, ok := stringKind(value); ok {
		switch strings.ToLower(text) {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		}
		return false, false
	}
	if number, ok := toInt64(value); ok {
		switch number {
		case 0:
			return false, true
		case 1:
			return true, true
		}
	}
	return false, false
}

// stringKind 取出字符串形态的值（string / []byte / json.Number 等）。
func stringKind(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed), true
	case []byte:
		return strings.TrimSpace(string(typed)), true
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.String {
		return strings.TrimSpace(reflected.String()), true
	}
	return "", false
}

// maxMetadataTextLen 是兜底字段的长度上限。
//
// 防的是「某个键里塞了一整段正文」：字段越长，BM25 的长度归一化越会把命中算得
// 不值钱，结果恰好相反 —— 元数据写得越全的文档反而排到后面。超长时截断而不是
// 整段丢弃：前缀里的型号、品牌名通常才是要搜的东西。
const maxMetadataTextLen = 4096

// FlattenMetadata 把整份元数据摊平成一段可检索文本。
//
// 摊平是「新增元数据键不必改 mapping」这条保证的落点：ES 的 mapping 一旦要改，
// 就得重建索引、重跑全部文档；把没有单独成列的值汇总进一个 text 字段，新键从
// 写入那一刻起就可检索，代价只是它们共享一个较低的权重。
//
// 它**不做**「哪些值已经单独成列」的排除 —— 那需要知道映射声明，见
// Mapping.Project。直接拿它对着一份已经按声明取过值的元数据用，会把同一个值
// 索引两遍。
//
// 调用方负责先把链路机制键摘掉（doc_id / content_hash / visibility / …）：
// 那是业务语义，不该由存储层猜。
func FlattenMetadata(metadata map[string]any) string {
	var builder strings.Builder
	appendMetadataValues(&builder, metadata, "", nil)
	return builder.String()
}

// Project 按声明把元数据投影成两部分：单独成列的字段，与摊平的兜底文本。
//
// 两者必须由同一次判断算出来。让调用方各调一次 Extract 与 FlattenMetadata，
// 兜底文本就会把已经单独成列的值**再收一遍** —— 同一个词被两个字段索引，而且
// 是两个不同权重的字段。代价不是「多占一点空间」，而是排序失真：
//
// 中文业务词（「铰链」「固装」「图冠系列」）本来靠 IDF 会被压到几乎不影响排序，
// 因为全库文档都带它们，分辨力为零。重复计入把一部分文档的 tf 抬到别人的两三
// 倍，凭空造出一个「这篇更相关」的信号 —— 于是「问固装铰链，结果全被拽到只含
// 铰链的文档上」。同一个值在同一篇文档里出现两次，不增加任何召回，只扰乱排序。
//
// 排除只针对**确实取到值**的路径（见 claimedPaths）：声明了 from 但取不到值的
// 字段在索引里并不存在，把它的路径也算成「已成列」，那个值就会从检索面整体消失
// —— 配错一个路径不该把内容悄悄抹掉，那种症状比配错本身更难查。
func (m *Mapping) Project(metadata map[string]any) (map[string]any, string) {
	columns := m.Extract(metadata)
	if len(metadata) == 0 {
		return columns, ""
	}
	var builder strings.Builder
	appendMetadataValues(&builder, metadata, "", m.claimedPaths(metadata))
	return columns, builder.String()
}

// claimedPaths 返回已经单独成列的元数据路径（就是声明里的 from）。
//
// 复算一遍取值，而不是只看声明里写了 from：Project 的排除面必须与 Extract 的
// 取值面逐字对应，两处各自判断就会分叉 —— 分叉的表现是「某个字段配了但搜不到」，
// 而且只在部分文档上出现（取值成功与否取决于该文档有没有那个键）。
func (m *Mapping) claimedPaths(metadata map[string]any) map[string]struct{} {
	if len(m.Fields) == 0 {
		return nil
	}
	claimed := make(map[string]struct{}, len(m.Fields))
	for _, spec := range m.Fields {
		raw, ok := lookupPath(metadata, spec.From)
		if !ok {
			continue
		}
		if _, ok := coerce(raw, spec); !ok {
			continue
		}
		claimed[spec.From] = struct{}{}
	}
	if len(claimed) == 0 {
		return nil
	}
	return claimed
}

// appendMetadataValues 摊平一层元数据。
//
// 键按字典序处理，而不是 map 的遍历顺序：同一份元数据必须每次拍出同样的文本，
// 否则索引文档的 _source 会随进程抖动，比对两次写入差异、复现问题都变得困难。
//
// prefix 是这一层所处的路径，claimed 里是已经单独成列的路径。路径只在 map 逐层
// 下降时才有意义 —— 映射声明也不认数组下标（见 chunk_mapping.yaml）。
func appendMetadataValues(
	builder *strings.Builder,
	metadata map[string]any,
	prefix string,
	claimed map[string]struct{},
) {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if _, ok := claimed[path]; ok {
			continue
		}
		appendMetadataValue(builder, metadata[key], path, claimed)
	}
}

// appendMetadataValue 递归收集标量值。path 是这个值在元数据里的路径。
//
// 递归是必需的：规格明细（specs_from_doc）是嵌套一层 map，型号变体（variants）
// 是数组。只处理顶层字符串的话，「门板材质是拉丝不锈钢」这类最常被搜的内容
// 反而进不了索引 —— 它们全在嵌套里。
//
// 数组元素沿用容器自己的路径：映射声明不认下标，所以「这个数组整体已经成列」
// 这件事只能落在容器路径上，在调用它的那层就判断掉了。
//
// 只收值、不收键：键是 cup_diameter_mm 这类英文标识，不是用户的搜索词。
// 布尔值也丢掉，true / false 命中「真」「假」之类的查询只会造噪声。
func appendMetadataValue(
	builder *strings.Builder,
	value any,
	path string,
	claimed map[string]struct{},
) {
	if builder.Len() >= maxMetadataTextLen {
		return
	}
	value = deref(value)
	if value == nil {
		return
	}
	switch typed := value.(type) {
	case bool:
		return
	case map[string]any:
		appendMetadataValues(builder, typed, path, claimed)
		return
	}
	if items, ok := asSlice(value); ok {
		for _, item := range items {
			appendMetadataValue(builder, item, path, claimed)
		}
		return
	}
	// 数字与其它标量：规格里的「开合角度 110」「门板厚度 18」都是这种，
	// 兜住它们比区分类型重要。
	if text, ok := toString(value); ok {
		appendMetadataToken(builder, text)
	}
}

// appendMetadataToken 追加一个词元，按剩余额度截断。
func appendMetadataToken(builder *strings.Builder, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	remaining := maxMetadataTextLen - builder.Len()
	if remaining <= 0 {
		return
	}
	if len(value) > remaining {
		// 按字节截断可能切在 UTF-8 字符中间，但这段文本只用来分词检索，
		// 半个字符会在分词时被丢弃，不影响正确性。
		value = value[:remaining]
	}
	if builder.Len() > 0 {
		builder.WriteString(" ")
	}
	builder.WriteString(value)
}

// sortedNames 返回 map 的键，升序，用于拼可读的报错信息。
func sortedNames(values map[string]float64) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func intPtr(value int) *int { return &value }

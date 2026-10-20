package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const (
	DefaultAPIConfig          = "./configs/api/config.yaml"
	DefaultAPITransportConfig = "./configs/api/restapi.yaml"
	DefaultWorkerConfig       = "./configs/worker/config.yaml"
)

type configProfile string

const (
	profileLegacy configProfile = "legacy"
	profileAPI    configProfile = "api"
	profileWorker configProfile = "worker"
)

// Each entry point has an independent override. EINO_CONFIG is intentionally
// not a fallback: a leftover shared override must not make both processes read
// the same file again.
func APIConfigPath() string { return configPath("EINO_API_CONFIG", DefaultAPIConfig) }
func APITransportConfigPath() string {
	return configPath("EINO_REST_CONFIG", DefaultAPITransportConfig)
}
func WorkerConfigPath() string { return configPath("EINO_WORKER_CONFIG", DefaultWorkerConfig) }
func configPath(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// validateConfigFile 是解析前的守门人，做三件事，全部在读文件之后、连任何外部
// 依赖之前完成：
//
//  1. 未知键检测。配置键名的唯一出处是 Config 上的 json tag（大驼峰，见
//     config.go 顶部注释）。go-zero 的 mapping 只做「大小写不敏感 + 精确拼写」
//     的匹配：`Host` / `HOST` 都能命中，`HOST_NAME`、`max_iterations` 这类
//     老写法一个字都对不上，而**对不上不会报错**，只会让那一项永远取零值 ——
//     症状是「配了但没生效」，不是启动失败。所以这里按 tag 树逐个键校验。
//  2. 角色边界：api / worker 各自只允许自己那一段，混进另一进程的段直接失败。
//  3. 段内禁配字段：例如 worker 文件里不得出现 API 专属的 Knowledge.DefaultTopK。
//
// 真正的解析交给 go-zero 的 conf（按扩展名选解析器，yaml / json 通用）。
func validateConfigFile(path string, profile configProfile) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml", ".json":
	default:
		return fmt.Errorf(
			"%s config must be a .yaml, .yml or .json file: %s", profile, path,
		)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if root.Kind == 0 { // 空文件：缺什么由 go-zero 的必填校验去说
		return nil
	}
	doc := &root
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return fmt.Errorf("%s config must be a mapping: %s", profile, path)
	}

	if err := validateKnownKeys(doc, profile, "", configKeyTree()); err != nil {
		return err
	}
	if profile == profileLegacy {
		return nil
	}
	return validateProfileSections(doc, profile)
}

// validateProfileSections 只放行当前角色拥有的段与字段。
func validateProfileSections(doc *yaml.Node, profile configProfile) error {
	allowed := map[string]bool{
		"storage": true, "observability": true, "knowledge": true,
		"embedding": true, "milvus": true, "es": true, "asynq": true, "redis": true,
	}
	if profile == profileAPI {
		for _, key := range []string{"server", "runtime", "agent", "model", "context", "auth", "maintenance", "retrieval"} {
			allowed[key] = true
		}
	} else {
		allowed["indexer"] = true
		allowed["maintenance"] = true
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		rawKey, node := doc.Content[i].Value, doc.Content[i+1]
		key := strings.ToLower(rawKey)
		if !allowed[key] {
			return fmt.Errorf("%s config must not contain %q section", profile, rawKey)
		}
		var forbidden []string
		if key == "observability" {
			forbidden = []string{"workerServiceName"}
		}
		if profile == profileAPI {
			switch key {
			case "knowledge":
				forbidden = []string{"root", "chunkSizeCharacters", "chunkOverlapCharacters", "maxChunksPerDocument"}
			case "asynq":
				forbidden = []string{"queues", "concurrency", "retryDelaySeconds", "maxRetryDelaySeconds", "shutdownTimeoutSeconds"}
			}
		} else if key == "knowledge" {
			forbidden = []string{"defaultTopK", "maxTopK", "maxQueryCharacters", "maxResultBytes", "recallGrouping"}
		}
		if profile == profileWorker {
			if key == "milvus" {
				forbidden = []string{"topKCandidate", "searchTimeoutMS"}
			}
			if key == "observability" {
				forbidden = []string{"workerServiceName", "metricsEnabled"}
			}
		}
		for j := 0; node.Kind == yaml.MappingNode && j+1 < len(node.Content); j += 2 {
			field := node.Content[j].Value
			normalizedField := strings.ToLower(field)
			for _, name := range forbidden {
				if normalizedField == strings.ToLower(name) {
					return fmt.Errorf("%s config must not contain %s.%s", profile, rawKey, field)
				}
			}
			// Keep the worker indexer section strict so a misspelling cannot
			// silently disable indexing. Other sections intentionally allow
			// fields consumed by newer components that share this config file.
			if profile == profileWorker && key == "indexer" && normalizedField != "batchsize" {
				return fmt.Errorf("%s config must not contain %s.%s", profile, rawKey, field)
			}
		}
	}
	return nil
}

// keyTree 是 Config 结构体声明的配置键树。键名保留 json tag 的原始拼写（错误
// 信息里要按它提示），匹配时统一按大小写不敏感处理 —— 与 go-zero 的 mapping
// 保持一致：大小写不影响绑定，拼写才影响。
//
// 三种节点：
//   - children 非 nil：这一层只允许这些键，逐个往下校验；
//   - elem 非 nil：这一层的键是数据（map[string]Struct 的账号名之类），
//     但每个值仍按 elem 校验；
//   - open：整段都是数据（map[string]string、标量），不校验子键。
type keyTree struct {
	name     string // json tag 的原始拼写，只用于错误提示
	children map[string]*keyTree
	elem     *keyTree
	open     bool
}

func (t *keyTree) child(name string) (*keyTree, bool) {
	if t.children == nil {
		return nil, false
	}
	child, ok := t.children[strings.ToLower(name)]
	return child, ok
}

// suggest 只在大小写不敏感匹配失败后调用：把键名和候选名都压成
// 「小写 + 只留字母数字」，好让 HOST_NAME / max_iterations 这类旧写法能拿到
// 一句「你是不是想写 X」。
func (t *keyTree) suggest(name string) string {
	normalized := normalizeKey(name)
	for _, candidate := range t.children {
		if normalized == normalizeKey(candidate.name) {
			return candidate.name
		}
	}
	return ""
}

func normalizeKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	configKeyTreeOnce sync.Once
	configKeyTreeVal  *keyTree
)

func configKeyTree() *keyTree {
	configKeyTreeOnce.Do(func() {
		configKeyTreeVal = buildKeyTree(reflect.TypeOf(Config{}), map[reflect.Type]bool{})
	})
	return configKeyTreeVal
}

func buildKeyTree(t reflect.Type, seen map[reflect.Type]bool) *keyTree {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if seen[t] { // 自引用保护：只可能是 *Config 之类的递归结构
			return &keyTree{open: true}
		}
		seen[t] = true
		defer delete(seen, t)
		children := make(map[string]*keyTree, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			name := jsonKeyName(field)
			if name == "-" {
				continue
			}
			child := buildKeyTree(field.Type, seen)
			child.name = name
			children[strings.ToLower(name)] = child
		}
		return &keyTree{children: children}
	case reflect.Slice, reflect.Array:
		return buildKeyTree(t.Elem(), seen)
	case reflect.Map:
		// map[string]Struct：键是数据（账号名之类），值仍要按结构体校验；
		// map[string]string 这类：整段都是数据，不校验子键。
		elem := t.Elem()
		for elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}
		if elem.Kind() == reflect.Struct {
			return &keyTree{elem: buildKeyTree(elem, seen)}
		}
		return &keyTree{open: true}
	default:
		return &keyTree{}
	}
}

// jsonKeyName 返回 go-zero 用来绑定字段的键名：json tag 里逗号前的部分，空则
// 用字段名本身（`json:",optional"` 与 `json:"Host,optional"` 等价）。
func jsonKeyName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return field.Name
	}
	return name
}

func validateKnownKeys(node *yaml.Node, profile configProfile, path string, tree *keyTree) error {
	switch node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if err := validateKnownKeys(item, profile, path, tree); err != nil {
				return err
			}
		}
		return nil
	case yaml.AliasNode:
		return validateKnownKeys(node.Alias, profile, path, tree)
	case yaml.MappingNode:
		if tree.elem != nil { // 键是数据，值是结构体：键照抄进路径，值按 tag 校验
			for i := 0; i+1 < len(node.Content); i += 2 {
				child := node.Content[i].Value
				full := child
				if path != "" {
					full = path + "." + child
				}
				if err := validateKnownKeys(node.Content[i+1], profile, full, tree.elem); err != nil {
					return err
				}
			}
			return nil
		}
		if tree.open {
			return nil
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			child, ok := tree.child(key)
			full := key
			if path != "" {
				full = path + "." + key
			}
			if !ok {
				hint := ""
				if suggestion := tree.suggest(key); suggestion != "" {
					hint = fmt.Sprintf(" (did you mean %q?)", suggestion)
				}
				return fmt.Errorf(
					"%s config contains unknown key %q%s", profile, full, hint,
				)
			}
			if err := validateKnownKeys(node.Content[i+1], profile, full, child); err != nil {
				return err
			}
		}
		return nil
	default:
		if len(tree.children) > 0 || tree.elem != nil {
			return fmt.Errorf("%s config expects a mapping at %q", profile, path)
		}
		return nil
	}
}

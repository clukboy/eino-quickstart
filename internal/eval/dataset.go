package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// LoadCases 按顺序装载一个或多个 JSONL 用例文件。
//
// 多文件是拼成全量一起跑的，不是各跑各的：拆文件只是为了按场景维护方便，
// 指标必须建立在完整用例集上 —— 分文件各自算 PassRate 会把小集合的波动
// 放大成"某个场景退化"的假象。
func LoadCases(paths ...string) ([]Case, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("eval: 用例集路径为空")
	}

	cases := make([]Case, 0, 32)
	// 记录每个 id 来自哪个文件：重复 id 必须报出来，否则一条用例被覆盖或
	// 计两次，指标会悄悄失真。
	origin := make(map[string]string, 32)

	for _, path := range paths {
		loaded, err := loadJSONL(path)
		if err != nil {
			return nil, err
		}
		for index, item := range loaded {
			if err := validateCase(item); err != nil {
				return nil, fmt.Errorf("%s 第 %d 条: %w", path, index+1, err)
			}
			if previous, exists := origin[item.ID]; exists {
				return nil, fmt.Errorf("用例 id %q 重复：%s 与 %s", item.ID, previous, path)
			}
			origin[item.ID] = path
			cases = append(cases, item)
		}
	}

	if len(cases) == 0 {
		return nil, fmt.Errorf("eval: 用例集为空，无法评测")
	}
	return cases, nil
}

func loadJSONL(path string) ([]Case, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("eval: 打开用例集: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	// 用例里有长 query 与多个 source，默认 64KB 上限太紧。
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	cases := make([]Case, 0, 32)
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "//") {
			continue
		}
		var item Case
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, fmt.Errorf("eval: %s 第 %d 行解析失败: %w", path, line, err)
		}
		cases = append(cases, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("eval: 读取 %s: %w", path, err)
	}
	return cases, nil
}

func validateCase(item Case) error {
	if strings.TrimSpace(item.ID) == "" {
		return fmt.Errorf("id 不能为空")
	}
	if strings.TrimSpace(item.Query) == "" {
		return fmt.Errorf("query 不能为空")
	}
	if item.TopK < 0 {
		return fmt.Errorf("top_k 不能为负")
	}
	if item.MinResults < 0 {
		return fmt.Errorf("min_results 不能为负")
	}
	for _, source := range append(append([]string{}, item.Expected...), item.Forbidden...) {
		if strings.TrimSpace(source) == "" {
			return fmt.Errorf("expected/forbidden 里存在空 source")
		}
		if strings.HasPrefix(source, "/") {
			// 判定口径是 document.source（相对 knowledge.root）。写成绝对路径
			// 会永远匹配不上，而且是静默地永远匹配不上。
			return fmt.Errorf("source %q 必须是相对 knowledge.root 的路径", source)
		}
	}
	for _, keyword := range item.ExpectedKeywords {
		if strings.TrimSpace(keyword) == "" {
			return fmt.Errorf("expected_source_keywords 里存在空关键词")
		}
	}
	return nil
}

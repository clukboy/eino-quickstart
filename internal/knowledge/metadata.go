package knowledge

import (
	"eino-quickstart/internal/knowledge/types/product"
	"strings"
)

var topicRules = []struct {
	Topic    string
	Keywords []string
}{
	{
		Topic: "installation",
		Keywords: []string{
			"安装",
			"装配",
			"安装方式",
			"安装步骤",
			"安装方法",
			"安装说明",
		},
	},
	{
		Topic: "installation_notice",
		Keywords: []string{
			"安装注意",
			"安装须知",
			"注意事项",
			"安装要求",
		},
	},
	{
		Topic: "troubleshooting",
		Keywords: []string{
			"常见问题",
			"安装问题",
			"故障",
			"故障排查",
			"问题处理",
			"异常",
			"异响",
			"解决方法",
			"怎么办",
		},
	},
	{
		Topic: "specification",
		Keywords: []string{
			"产品参数",
			"技术参数",
			"规格",
			"参数",
		},
	},
	{
		Topic: "product_information",
		Keywords: []string{
			"产品介绍",
			"产品概述",
			"产品说明",
			"简介",
		},
	},
	{
		Topic: "adjustment",
		Keywords: []string{
			"调节",
			"调整",
			"调节方法",
			"调整方法",
		},
	},
	{
		Topic: "maintenance",
		Keywords: []string{
			"维护",
			"保养",
			"维护说明",
			"保养说明",
		},
	},
	{
		Topic: "usage",
		Keywords: []string{
			"使用方法",
			"使用说明",
			"使用",
		},
	},
	{
		Topic: "compatibility",
		Keywords: []string{
			"适配",
			"兼容",
			"适用",
		},
	},
	{
		Topic: "safety",
		Keywords: []string{
			"安全",
			"安全注意",
			"警告",
			"禁止",
		},
	},
	{
		Topic: "faq",
		Keywords: []string{
			"FAQ",
			"常见问答",
			"问答",
		},
	},
}

func BuildChunkMetadata(documentMetadata map[string]any, productInfo *product.Product, headingPath string, content string) map[string]any {
	result := cloneMap(documentMetadata)
	if result == nil {
		result = make(map[string]any)
	}

	if productInfo != nil {
		for key, value := range productInfo.ToMap() {
			if value == nil {
				continue
			}
			result[key] = value
		}
	}
	topics := DetectTopics(headingPath, content)
	if len(topics) > 0 {
		result["topics"] = topics
	}
	if headingPath != "" {
		result["heading_path"] = splitHeadingPath(headingPath)
	}
	return result
}

func DetectTopics(headingPath string, content string) []string {
	text := strings.ToLower(strings.TrimSpace(headingPath + "\n" + content))
	if text == "" {
		return nil
	}
	result := make([]string, 0)
	seen := make(map[string]struct{})
	for _, rule := range topicRules {
		for _, keyword := range rule.Keywords {
			if strings.Contains(text, strings.ToLower(keyword)) {
				if _, exists := seen[rule.Topic]; exists {
					break
				}
				result = append(result, rule.Topic)
				seen[rule.Topic] = struct{}{}
				break
			}
		}
	}
	return result
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return make(map[string]any)
	}
	result := make(map[string]any, len(input))
	for k, v := range input {
		result[k] = v
	}
	return result
}

func splitHeadingPath(value string) []string {
	parts := strings.Split(value, ">")

	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

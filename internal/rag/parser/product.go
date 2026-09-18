package parser

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/components/document/parser"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

const (
	// MetaKeySource is the metadata key storing the document's source URI.
	MetaKeySource = "_source"

	// TypeProduct 是 ProductParser 在解析器注册表里的名字，也是数据集
	// dataset.type 要填的值。两处必须是同一个字符串：填错不会报错，只会
	// 静默退化成 TextParser，产品块的 YAML 头没人解析。
	TypeProduct = "product"
)

// Block 是文件里的一个产品块。
type Block struct {
	// Raw 是块在原文里的逐字文本（含 YAML 头）。
	//
	// 留着它的意义是「一个产品一个文件」：把一份多产品文件拆成 N 个文档之后，
	// 每个文档的正文就是这个值，写回磁盘仍能被同一个解析器读回来 —— 拆出去
	// 的每一份都是自洽的，不依赖原来那份大文件。
	Raw string

	// Metadata 是 YAML 头解析出的产品元数据。
	Metadata map[string]any

	// Content 是去掉 YAML 头之后的 Markdown 正文。
	Content string
}

// SplitBlocks 把一份多产品文件拆成若干产品块。
//
// 请求期（按产品建文档）与 worker 期（解析单产品文件）都走这里，不允许各写
// 一套：两处对「什么算一个产品块」的理解一旦分叉，症状是「上传时看到 10 条、
// 索引后只剩 1 条」这类对不上的现象，而两边的代码各自看都没问题。
//
// 边界与 ProductParser 完全一致：只有以 ```yaml 围栏开头的块会被收集，围栏
// 之前的内容（标题、说明）不是产品块。
func SplitBlocks(content string) ([]Block, error) {
	raw := splitProducts(content)
	blocks := make([]Block, 0, len(raw))
	for _, block := range raw {
		rest, ok := strings.CutPrefix(block, "```yaml")
		if !ok {
			return nil, fmt.Errorf("product block does not start with a ```yaml fence")
		}
		endIdx := strings.Index(rest, "\n```")
		if endIdx == -1 {
			// 兼容 --- 后面没有换行的极端情况
			endIdx = strings.Index(rest, "```")
			if endIdx == -1 {
				return nil, fmt.Errorf("missing closing front matter delimiter '```'")
			}
		}

		// 4. 提取 YAML 部分（去掉首尾空白）
		yamlStr := strings.TrimSpace(rest[:endIdx])

		// 5. 提取 Markdown 部分
		//    跳过 "\n```yaml" (4字节) 或 "```" (3字节)
		mdStart := endIdx + 4
		if mdStart > len(rest) {
			mdStart = endIdx + 3
		}

		pm, err := yamlToProduct(yamlStr)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, Block{
			Raw:      block,
			Metadata: pm.toMap(),
			Content:  strings.TrimSpace(rest[mdStart:]),
		})
	}
	return blocks, nil
}

// ProductParser 定义product都使用的parser，主要是为了在parser中添加source信息，方便后续的检索和引用。
type ProductParser struct{}

// Parse 把文件拆成「一个产品块一个 schema.Document」。
//
// 注意粒度：这里拆出来的每个 Document 对应文件里的一个产品，但落到
// documents 表时未必是一条 —— 是否按产品建文档由应用层决定（见
// internal/application/knowledge）。解析层只负责如实拆开。
func (dp ProductParser) Parse(ctx context.Context, reader io.Reader, opts ...parser.Option) ([]*schema.Document, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	blocks, err := SplitBlocks(string(data))
	if err != nil {
		return nil, err
	}
	docs := make([]*schema.Document, 0, len(blocks))
	opt := parser.GetCommonOptions(&parser.Options{}, opts...)

	for _, block := range blocks {
		meta := block.Metadata
		meta[MetaKeySource] = opt.URI

		for k, v := range opt.ExtraMeta {
			meta[k] = v
		}
		docs = append(docs, &schema.Document{
			Content:  block.Content,
			MetaData: meta,
		})
	}

	return docs, nil
}

type productMetadata struct {
	ProductId    string `yaml:"product_id"`
	Model        string `yaml:"model"`
	FamilyPrefix string `yaml:"family_prefix"`
	SeriesName   string `yaml:"series_name"`
	ProductName  string `yaml:"product_name"`
	CategoryL1   string `yaml:"category_l1"`
	CategoryL2   string `yaml:"category_l2"`
	SpecsFromDoc struct {
		MechanicsType      string `yaml:"mechanics_type"`
		InstallType        string `yaml:"install_type"`
		AdjustType         string `yaml:"adjust_type"`
		DoorMaterial       string `yaml:"door_material"`
		BaseMaterial       string `yaml:"base_material"`
		SurfaceFinish      string `yaml:"surface_finish"`
		OpenAngleDeg       *int   `yaml:"open_angle_deg"`
		CupDiameterMm      *int   `yaml:"cup_diameter_mm"`
		DoorThicknessMinMm *int   `yaml:"door_thickness_min_mm"`
		DoorThicknessMaxMm *int   `yaml:"door_thickness_max_mm"`
	} `yaml:"specs_from_doc"`
	Variants  []string `yaml:"variants"`
	SourceDoc string   `yaml:"source_doc"`
}

// splitProducts 将多产品长文本按 Front Matter 边界切分为 []string
// 每个元素是一个完整的 "YAML + Markdown" 产品块。
//
// 每个块都保证以规范的 "```yaml" 开头（缩进过的围栏会被归一），这样块本身
// 就是一个合法的单产品文件 —— 拆出来的每一份都能独立再解析一次。
func splitProducts(content string) []string {
	content = strings.TrimLeft(content, "\ufeff \t\n\r")
	lines := strings.Split(content, "\n")

	var results []string
	var current strings.Builder
	inBlock := false // 标记是否正在收集一个完整的产品块

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "```yaml" {
			// 如果之前已经在收集，说明上一个产品块结束了
			if inBlock && current.Len() > 0 {
				results = append(results, strings.TrimSpace(current.String()))
				current.Reset()
			}
			inBlock = true
			current.WriteString("```yaml")
			current.WriteString("\n")
			continue
		}

		// 只在产品块内收集内容
		if inBlock {
			current.WriteString(line)
			current.WriteString("\n")
		}
	}

	// 处理最后一个产品（文件末尾没有多余的 ---）
	if inBlock && current.Len() > 0 {
		results = append(results, strings.TrimSpace(current.String()))
	}

	return results
}
func yamlToProduct(content string) (productMetadata, error) {
	var pm productMetadata
	if err := yaml.Unmarshal([]byte(content), &pm); err != nil {
		return pm, err
	}
	return pm, nil
}

func (pm productMetadata) toMap() map[string]any {
	return map[string]any{
		"product_id":    pm.ProductId,
		"model":         pm.Model,
		"family_prefix": pm.FamilyPrefix,
		"series_name":   pm.SeriesName,
		"product_name":  pm.ProductName,
		"category_l1":   pm.CategoryL1,
		"category_l2":   pm.CategoryL2,
		"specs_from_doc": map[string]any{
			"mechanics_type":        pm.SpecsFromDoc.MechanicsType,
			"install_type":          pm.SpecsFromDoc.InstallType,
			"adjust_type":           pm.SpecsFromDoc.AdjustType,
			"door_material":         pm.SpecsFromDoc.DoorMaterial,
			"base_material":         pm.SpecsFromDoc.BaseMaterial,
			"surface_finish":        pm.SpecsFromDoc.SurfaceFinish,
			"open_angle_deg":        pm.SpecsFromDoc.OpenAngleDeg,
			"cup_diameter_mm":       pm.SpecsFromDoc.CupDiameterMm,
			"door_thickness_min_mm": pm.SpecsFromDoc.DoorThicknessMinMm,
			"door_thickness_max_mm": pm.SpecsFromDoc.DoorThicknessMaxMm,
		},
		"variants":   pm.Variants,
		"source_doc": pm.SourceDoc,
	}
}

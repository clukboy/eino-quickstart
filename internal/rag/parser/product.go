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
)

// ProductParser 定义product都使用的parser，主要是为了在parser中添加source信息，方便后续的检索和引用。
type ProductParser struct{}

// Parse reads the text from a reader and returns a single document.
func (dp ProductParser) Parse(ctx context.Context, reader io.Reader, opts ...parser.Option) ([]*schema.Document, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	products := splitProducts(string(data))
	// 开始拆分文档数据
	docs := make([]*schema.Document, 0, len(products))
	opt := parser.GetCommonOptions(&parser.Options{}, opts...)

	for _, content := range products {
		rest := content[3:]
		endIdx := strings.Index(rest, "\n---")
		if endIdx == -1 {
			// 兼容 --- 后面没有换行的极端情况
			endIdx = strings.Index(rest, "---")
			if endIdx == -1 {
				return nil, fmt.Errorf("missing closing front matter delimiter '---'")
			}
		}

		// 4. 提取 YAML 部分（去掉首尾空白）
		yamlStr := strings.TrimSpace(rest[:endIdx])

		// 5. 提取 Markdown 部分
		//    跳过 "\n---" (4字节) 或 "---" (3字节)
		mdStart := endIdx + 4
		if mdStart > len(rest) {
			mdStart = endIdx + 3
		}
		markdownStr := strings.TrimSpace(rest[mdStart:])

		pm, err := yamlToProduct(yamlStr)
		if err != nil {
			return nil, err
		}
		meta := pm.toMap()
		meta[MetaKeySource] = opt.URI

		for k, v := range opt.ExtraMeta {
			meta[k] = v
		}
		docs = append(docs, &schema.Document{
			Content:  markdownStr,
			MetaData: meta,
		})

		// 每个文档都写入到 postgresql存储
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

// SplitProducts 将多产品长文本按 Front Matter 边界切分为 []string
// 每个元素是一个完整的 "YAML + Markdown" 产品块
func splitProducts(content string) []string {
	content = strings.TrimLeft(content, "\ufeff \t\n\r")
	lines := strings.Split(content, "\n")

	var results []string
	var current strings.Builder
	delimiterCount := 0
	inBlock := false // 标记是否正在收集一个完整的产品块

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "---" {
			delimiterCount++

			// 奇数次 ---：新产品块的开始
			if delimiterCount%2 != 0 {
				// 如果之前已经在收集，说明上一个产品块结束了
				if inBlock && current.Len() > 0 {
					results = append(results, strings.TrimSpace(current.String()))
					current.Reset()
				}
				inBlock = true
				current.WriteString(line + "\n")
				continue
			}

			// 偶数次 ---：YAML 结束，Markdown 开始，继续收集
			current.WriteString(line + "\n")
			continue
		}

		// 只在产品块内收集内容
		if inBlock {
			current.WriteString(line + "\n")
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

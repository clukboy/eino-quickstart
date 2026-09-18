package knowledge

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"eino-quickstart/internal/rag"
	"eino-quickstart/internal/rag/constant"
	ragparser "eino-quickstart/internal/rag/parser"
)

// productStemSuffixLen 是文件名主干里补在可读前缀之后的哈希长度。
//
// 8 个十六进制字符（32 位）足够：同一个数据集里的产品数量远到不了 $2^{16}$，
// 生日碰撞概率可以忽略，而文件名还保持人能读的长度。
const productStemSuffixLen = 8

// productDocument 是一个待落库的产品文档规格。
//
// 它是「一份文件拆成 N 条文档」这件事的中间形态：拆分只产出规格，落盘、建
// 行、投递队列由 Service 决定 —— 这样拆分本身是纯函数，可以单独测。
type productDocument struct {
	// Key 是数据集内的查重键（documents.external_key）。空表示这个块没有型号
	// 信息，不参与查重，每次上传都会新建一条。
	Key string

	// Title 是文档标题，取产品名（有型号前缀）。
	Title string

	// Stem 是托管目录里的文件名主干，不含扩展名。
	Stem string

	// Content 是单产品文件的正文，也就是块在原文里的逐字文本（含 YAML 头）。
	// 保持逐字是为了让拆出来的每一份都能被同一个解析器再读一遍。
	Content string

	// Metadata 是产品的业务元数据，整份落到 documents.metadata 上。
	Metadata map[string]any
}

// buildProductDocuments 把一份多产品文件拆成一份份单产品文档规格。
//
// 返回空切片表示「正文里一个产品块都没有」，这不是错误 —— 普通 Markdown 上传
// 到产品数据集时就是这样。调用方应当退回「整份文件一条文档」，而不是把文件
// 丢掉：文件在列表里可见、状态诚实，比凭空消失好。
//
// 返回错误只有一种原因：文件里有产品块，但块本身坏掉了（围栏不闭合、YAML 不
// 合法、型号重复）。这些在请求期报出来，好过等到 worker 解析失败之后从文档
// 状态里倒查。
func buildProductDocuments(content, fallbackTitle string) ([]productDocument, error) {
	blocks, err := ragparser.SplitBlocks(content)
	if err != nil {
		return nil, invalid("解析产品块失败：%v", err)
	}
	if len(blocks) == 0 {
		return nil, nil
	}

	specs := make([]productDocument, 0, len(blocks))
	firstSeen := make(map[string]int, len(blocks))
	for index, block := range blocks {
		key := productKey(block.Metadata)
		if key != "" {
			if first, duplicated := firstSeen[key]; duplicated {
				return nil, invalid(
					"同一份文件里出现了重复的产品型号 %q（第 %d 个与第 %d 个产品块）：型号是查重键，重复会让后一个静默覆盖前一个",
					key, first+1, index+1,
				)
			}
			firstSeen[key] = index
		}
		specs = append(specs, productDocument{
			Key:      key,
			Title:    productTitle(block.Metadata, fallbackTitle, index),
			Stem:     productStem(key),
			Content:  block.Raw,
			Metadata: block.Metadata,
		})
	}
	return specs, nil
}

// productKey 取产品块的查重键。
//
// product_id 优先、缺失时退回 model。两者在多数资料里是同一个值，但语义不同：
// product_id 是产品的稳定标识，model 是型号（改款后可能变）。都取不到就返回空，
// 表示这个块不参与查重 —— 宁可每次都新建一条，也不要用一个会变的键把两个不同
// 的产品判成同一个。
func productKey(metadata map[string]any) string {
	for _, key := range []string{constant.MetaProductID, constant.MetaModel} {
		if value := metaString(metadata, key); value != "" {
			return value
		}
	}
	return ""
}

// productTitle 取文档标题。
//
// 型号 + 产品名，而不是上传的文件名：文件名对所有产品都一样（一份型录文件里
// 的每个产品都来自它），放进标题等于没有信息，列表页会变成一排重复的条目。
func productTitle(metadata map[string]any, fallback string, index int) string {
	model := metaString(metadata, constant.MetaModel)
	name := metaString(metadata, constant.MetaProductName)

	switch {
	case model != "" && name != "":
		return model + " " + name
	case name != "":
		return name
	case model != "":
		return model
	}
	if fallback = strings.TrimSpace(fallback); fallback != "" {
		return fmt.Sprintf("%s 第 %d 个产品", fallback, index+1)
	}
	return fmt.Sprintf("未命名产品 %d", index+1)
}

// productStem 生成托管目录里的文件名主干。
//
// 有型号时是「型号的可读前缀 + 键的短哈希」：前缀给人看（在文件系统里能认出
// 是哪个产品），哈希保唯一。只靠前缀不行 —— rag.Slugify 会小写化、会在 64 字符
// 处截断，`H105P` 与 `h105p`、或者两个前 64 字符相同的长型号会撞成同一个文件，
// 后写的那份静静盖掉先写的，而两条文档行都还在，各自指着一份内容不对的文件。
//
// 没有型号时用随机后缀：这种块不参与查重，每次上传都是新文档，文件名必须唯一，
// 否则两条文档会共用一份文件 —— 改一条会悄悄改掉另一条，删一条会把另一条的
// 正文一起删掉。这里不能用内容哈希，同一份文件重传就会撞上。
func productStem(key string) string {
	if key == "" {
		return "product-" + randomToken()
	}
	return rag.Slugify(key) + "-" + shortHash(key)
}

// shortHash 是文本的 SHA-256 前若干位十六进制。
func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:productStemSuffixLen]
}

// randomToken 生成一段短的随机文件名词元。
func randomToken() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// 取不到随机数时退到纳秒时间戳：唯一性弱一档，但比两条文档共用一份
		// 文件安全 —— 那种情况下的损坏是静默的。
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf[:])
}

// metaString 读一个字符串型的元数据值。
func metaString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

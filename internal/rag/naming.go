package rag

import (
	"strconv"
	"strings"
	"unicode"
)

// 这个文件回答一个问题：一篇文档叫什么名字。
//
// 名字（documents.source）不再对应任何磁盘文件，但仍然是**对外可见的标识**：
// 检索结果要靠它做引用与去重、评测用例集要靠它写期望命中的文档、ES 里有它的
// 独立字段。所以它必须满足两条：
//
//   - 稳定：同一篇文档反复上传、正文反复改动，名字不变 —— 换一次内容不该让
//     引用失效，也不该让用例集活不过一次重传。
//   - 唯一：同一数据集内不能撞名，否则两条文档会共用同一个引用，检索里按它
//     去重就会把其中一条静默吃掉。
//
// 形状沿用「documents/<数据集 id>/<主干>.md」：它是一个路径的样子，因为历史
// 上它确实是 knowledge root 下的相对路径，现有语料、评测用例与 ES 里已经存着
// 这个形状的值。把它换成另一种编码只会让存量数据对不上，换不来任何东西。

// ManagedDir 是历史上托管正文所在的子目录名，也是 source 的第一段。
//
// 新写入不再往磁盘上放任何东西，这个常量只剩两个用途：拼 source，以及让遗留
// 数据的导入知道去 knowledge root 的哪个子目录下找（见 LegacyContentReader）。
const ManagedDir = "documents"

// DocumentSource 按数据集 id 与文件名主干拼出文档的逻辑标识。
//
// stem 由调用方决定：产品型录用型号派生的主干（同一型号每次都是同一个名字），
// 普通文档用标题派生的主干。
func DocumentSource(datasetID uint64, stem string) string {
	return ManagedDir + "/" + strconv.FormatUint(datasetID, 10) + "/" + stem + ".md"
}

// Slugify 把任意文本压成可做名字主干片段的形式：保留字母与数字（含中文），
// 其余折叠成连字符，最长 64 个字符。
//
// 两点必须知道，否则会踩到静默撞名：
//   - 结果是小写的，`H105P` 与 `h105p` 压出来是同一个字符串；
//   - 超过 64 字符会被截断，长标题可能截出相同的前缀。
//
// 所以它只适合做**可读前缀**，唯一性要靠调用方另补一段（本仓库补的是键的短
// 哈希，见 application/knowledge 的 productStem）。
func Slugify(title string) string {
	var b strings.Builder
	lastDash := true // 防止开头出现连字符
	for _, r := range strings.TrimSpace(title) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
		if b.Len() >= 64 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "document"
	}
	return out
}

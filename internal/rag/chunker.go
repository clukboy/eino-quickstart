package rag

import (
	"context"
	"eino-quickstart/internal/rag/constant"
	"strings"

	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/schema"
)

// MarkdownChunker 两级拆分：
//  1. 按 Markdown 标题切 section（保留 heading path）；
//  2. 超长 section 用滑动窗口细分，窗口边界优先对齐句子。
//
// [优化] 新增：代码围栏（```）感知 —— 不会把代码块从中间切开。
type MarkdownChunker struct {
	maxChars int
	overlap  int
}

func NewMarkdownChunker(maxChars int) *MarkdownChunker {
	if maxChars <= 0 {
		maxChars = 1200
	}
	return &MarkdownChunker{maxChars: maxChars, overlap: maxChars / 6}
}

func (c *MarkdownChunker) Transform(_ context.Context, docs []*schema.Document, _ ...document.TransformerOption) ([]*schema.Document, error) {
	out := make([]*schema.Document, 0, len(docs)*4)
	for _, d := range docs {
		chunks := c.chunkDocument(d)
		out = append(out, chunks...)
	}
	return out, nil
}

type section struct {
	headingPath string
	text        string
}

func (c *MarkdownChunker) chunkDocument(d *schema.Document) []*schema.Document {
	EnsureMeta(d)
	sections := splitMarkdownSections(d.Content)

	var chunks []*schema.Document
	idx := 0
	for _, sec := range sections {
		for _, piece := range c.splitLong(sec.text) {
			piece = strings.TrimSpace(piece)
			if piece == "" {
				continue
			}
			chunk := &schema.Document{
				ID:       d.ID,
				Content:  piece,
				MetaData: CopyMeta(d.MetaData),
			}
			chunk.MetaData[constant.MetaChunkIndex] = idx
			chunk.MetaData[constant.MetaHeadingPath] = sec.headingPath
			// [优化] 把 heading path 前置进 chunk 内容，提升 embedding 语义完整性
			if sec.headingPath != "" {
				chunk.Content = "# " + sec.headingPath + "\n\n" + piece
			}
			chunks = append(chunks, chunk)
			idx++
		}
	}
	return chunks
}

// splitMarkdownSections 按标题切分，维护 heading 层级栈；代码围栏内不切分。
func splitMarkdownSections(content string) []section {
	lines := strings.Split(content, "\n")

	var (
		stack   []string // heading 栈
		cur     strings.Builder
		out     []section
		inFence bool
	)

	flush := func() {
		text := cur.String()
		cur.Reset()
		if strings.TrimSpace(text) != "" {
			out = append(out, section{headingPath: strings.Join(stack, " > "), text: text})
		}
	}

	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)

		// [优化] 代码围栏感知
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			cur.WriteString(ln)
			cur.WriteString("\n")
			continue
		}
		if !inFence && strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			if level <= 6 && level < len(trimmed) && trimmed[level] == ' ' {
				flush()
				title := strings.TrimSpace(trimmed[level:])
				if level-1 < len(stack) {
					stack = stack[:level-1]
				}
				for len(stack) < level-1 {
					stack = append(stack, "")
				}
				stack = append(stack, title)
				continue
			}
		}
		cur.WriteString(ln)
		cur.WriteString("\n")
	}
	flush()
	if len(out) == 0 { // 无标题的纯文本：整体作为一个 section
		out = append(out, section{text: content})
	}
	return out
}

// splitLong 滑动窗口切分，[优化] 断点优先选择句子边界，其次段落，最后硬切。
func (c *MarkdownChunker) splitLong(text string) []string {
	if len([]rune(text)) <= c.maxChars {
		return []string{text}
	}
	runes := []rune(text)
	var (
		out   []string
		start int
	)
	for start < len(runes) {
		end := start + c.maxChars
		if end >= len(runes) {
			out = append(out, string(runes[start:]))
			break
		}
		// 在窗口尾部向前找句子/段落边界
		cut := end
		for i := end; i > start+c.maxChars/2; i-- {
			ch := runes[i]
			if ch == '\n' || ch == '。' || ch == '.' || ch == '！' || ch == '？' ||
				ch == '!' || ch == '?' || ch == '；' || ch == ';' {
				cut = i + 1
				break
			}
		}
		out = append(out, string(runes[start:cut]))
		next := cut - c.overlap
		if next <= start {
			next = start + 1 // 防死循环
		}
		start = next
	}
	return out
}

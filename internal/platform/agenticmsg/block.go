package agenticmsg

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
)

// 展示块类型。这些值是**对外契约**（REST 的 blocks[].type），改动等于改接口：
// 加了新类型前端会按「不认识的类型」跳过，删了或改名会让旧前端渲染不出来。
const (
	BlockText       = "text"
	BlockToolCall   = "tool_call"
	BlockToolResult = "tool_result"
	BlockImage      = "image"
	BlockAudio      = "audio"
	BlockVideo      = "video"
	BlockFile       = "file"
)

// Block 是一条消息里的一个展示单元，也是 session_messages.blocks 的存储单元。
//
// 它**不是** eino 的 ContentBlock：后者是模型协议的一部分（十几个字段、随版本
// 变），而这是给人看的东西。两者刻意解耦 —— 模型升级不该改 HTTP 契约，展示
// 需求也不该往协议结构上挂字段。
//
// 同样的取舍体现在「不落什么」上：
//   - 工具结果的**正文**不落（那是按当时 subject 授权召回的原文，落下来等于给
//     历史留了一条永久越权通道，体积也扛不住），只落摘要；
//   - 多媒体的 base64 不落，只落 URL / MIME / 文件名；
//   - reasoning 不落，思考过程不属于回答。
type Block struct {
	Type string `json:"type"`

	// Text 是 text 块的正文。
	Text string `json:"text,omitempty"`

	// Name 是 tool_call / tool_result 的工具名。
	Name string `json:"name,omitempty"`

	// Arguments 是 tool_call 的入参 JSON（按上限截断）。
	Arguments string `json:"arguments,omitempty"`

	// Summary 是 tool_result 的一句话摘要，例如「召回 3 条」。
	Summary string `json:"summary,omitempty"`

	// URL / MIMEType / Filename 描述多媒体块。
	URL      string `json:"url,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	Filename string `json:"filename,omitempty"`

	// index 是流式分片里的块序号（schema.StreamingMeta.Index），只用于把同一条
	// 消息的多个分片合并成一块，不参与序列化。非流式消息恒为 -1。
	index int
}

// Blocks 把一条 agentic 消息投影成展示块。
//
// maxBytes 限制 tool_call 入参的长度（<=0 表示不截断）。这里复用
// Context.MaxToolOutputBytes 的用意是：模型看到的入参本身就是截断过的，展示
// 不该比模型看到的更完整。
//
// 顺序被保留：模型先说话再调工具、或者调完工具再说话，blocks 的先后就是实际
// 发生的先后。前端按顺序渲染即可，不需要自己重排。
func Blocks(msg *schema.AgenticMessage, maxBytes int) []Block {
	if msg == nil {
		return nil
	}

	blocks := make([]Block, 0, len(msg.ContentBlocks))
	for _, block := range msg.ContentBlocks {
		if block == nil {
			continue
		}

		switch {
		case block.UserInputText != nil:
			blocks = AppendText(blocks, block.UserInputText.Text)

		case block.AssistantGenText != nil:
			blocks = AppendText(blocks, block.AssistantGenText.Text)

		case block.FunctionToolCall != nil:
			blocks = append(blocks, Block{
				Type:      BlockToolCall,
				Name:      block.FunctionToolCall.Name,
				Arguments: clampText(block.FunctionToolCall.Arguments, maxBytes),
				index:     blockIndex(block),
			})

		case block.FunctionToolResult != nil:
			blocks = append(blocks, toolResultBlock(block.FunctionToolResult, blockIndex(block)))

		case block.UserInputImage != nil:
			blocks = append(blocks, mediaBlock(BlockImage, block.UserInputImage.URL, block.UserInputImage.MIMEType, "", block))

		case block.AssistantGenImage != nil:
			blocks = append(blocks, mediaBlock(BlockImage, block.AssistantGenImage.URL, block.AssistantGenImage.MIMEType, "", block))

		case block.UserInputAudio != nil:
			blocks = append(blocks, mediaBlock(BlockAudio, block.UserInputAudio.URL, block.UserInputAudio.MIMEType, "", block))

		case block.AssistantGenAudio != nil:
			blocks = append(blocks, mediaBlock(BlockAudio, block.AssistantGenAudio.URL, block.AssistantGenAudio.MIMEType, "", block))

		case block.UserInputVideo != nil:
			blocks = append(blocks, mediaBlock(BlockVideo, block.UserInputVideo.URL, block.UserInputVideo.MIMEType, "", block))

		case block.AssistantGenVideo != nil:
			blocks = append(blocks, mediaBlock(BlockVideo, block.AssistantGenVideo.URL, block.AssistantGenVideo.MIMEType, "", block))

		case block.UserInputFile != nil:
			blocks = append(blocks, mediaBlock(
				BlockFile,
				block.UserInputFile.URL,
				block.UserInputFile.MIMEType,
				block.UserInputFile.Name,
				block,
			))
		}
	}

	return blocks
}

// AppendText 把一段文本并进块序列。
//
// 紧挨着上一块的 text 会并进同一块，而不是各自成块 —— 流式回答是逐字到达的，
// 一个字一个块会让一条消息变成几百个 JSON 对象，前端也没法把它当成一段话排版。
func AppendText(blocks []Block, text string) []Block {
	if text == "" {
		return blocks
	}

	if n := len(blocks); n > 0 && blocks[n-1].Type == BlockText {
		blocks[n-1].Text += text
		return blocks
	}

	return append(blocks, Block{Type: BlockText, Text: text, index: -1})
}

// Sealed 返回抹掉分片序号的副本，用于「一条流结束、并入整轮」的时刻。
//
// 分片序号只在**一条消息内部**唯一：同一轮里第一次模型调用和第二次模型调用
// 都可能出现 index=0 的 tool_call，不抹掉就会被当成同一个调用，参数被拼到
// 一起（表现为「一次检索带着两次查询的入参」）。
func Sealed(blocks []Block) []Block {
	if len(blocks) == 0 {
		return nil
	}

	sealed := make([]Block, len(blocks))
	copy(sealed, blocks)
	for i := range sealed {
		sealed[i].index = -1
	}

	return sealed
}

// Merge 把一批新块并入已有序列，用于流式分片。
//
// 分片之间靠 index 认亲：同一条消息里，同一个 StreamingMeta.Index 的 tool_call
// 分片是同一个调用（名字在第一片、入参逐片追加）。不这么做的话，一次工具调用
// 会变成十几个半截的 tool_call 块 —— 前端看到的就是十几个空参数调用。
func Merge(blocks []Block, incoming []Block) []Block {
	for _, block := range incoming {
		if block.Type == BlockText {
			blocks = AppendText(blocks, block.Text)
			continue
		}

		if block.index >= 0 {
			if merged := mergeByIndex(blocks, block); merged {
				continue
			}
		}

		blocks = append(blocks, block)
	}

	return blocks
}

// mergeByIndex 尝试把 block 并进同 index 的已有块；合并成功返回 true。
func mergeByIndex(blocks []Block, block Block) bool {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Type != block.Type || blocks[i].index != block.index {
			continue
		}

		if blocks[i].Name == "" {
			blocks[i].Name = block.Name
		}
		blocks[i].Arguments += block.Arguments
		if blocks[i].URL == "" {
			blocks[i].URL = block.URL
		}
		if blocks[i].MIMEType == "" {
			blocks[i].MIMEType = block.MIMEType
		}
		if blocks[i].Summary == "" {
			blocks[i].Summary = block.Summary
		}

		return true
	}

	return false
}

// FallbackText 给「只有非文本产物」的一轮算一句占位正文。
//
// 起因是个真 bug：turn.Store.Complete 收到空正文就不写 session_messages，
// 于是「模型只回了一张图」的一轮在历史里彻底消失（用户那句留着、回答没了，
// 刷新之后看起来像没答过）。有了占位，轮次就能正常落库，前端靠 blocks 渲染。
//
// 返回空串只代表「这一轮真的什么都没有」，调用方据此跳过写入。
func FallbackText(blocks []Block) string {
	for _, block := range blocks {
		switch block.Type {
		case BlockText:
			if strings.TrimSpace(block.Text) != "" {
				return block.Text
			}
		case BlockToolResult, BlockToolCall:
			// 只有工具往返、没有正文：这不算「有产出」，仍然按空轮次处理。
		default:
			return "[" + block.Type + "]"
		}
	}

	return ""
}

// toolResultBlock 把工具结果压成一句摘要。
//
// 认「召回条数」靠的是本仓库知识库工具的输出约定（每条来源以 `[source#chunk-N]`
// 开头）。这是个**展示层的启发式**：工具换了输出格式，这里只会退化成
// 「返回 N 字符」，功能不受影响，也不会因为解析失败而报错 —— 所以不用 JSON 解析
// 去硬绑格式。
func toolResultBlock(result *schema.FunctionToolResult, index int) Block {
	block := Block{Type: BlockToolResult, Name: result.Name, index: index}

	text := toolResultText(result)
	if text == "" {
		return block
	}

	if hits := countCitations(text); hits > 0 {
		block.Summary = fmt.Sprintf("召回 %d 条", hits)
		return block
	}

	block.Summary = fmt.Sprintf("返回 %d 字符", utf8.RuneCountInString(text))
	return block
}

// toolResultText 把工具结果里的文本分片拼起来（只用于量体积与数来源）。
func toolResultText(result *schema.FunctionToolResult) string {
	if result == nil {
		return ""
	}

	var sb strings.Builder
	for _, part := range result.Content {
		if part == nil || part.Text == nil {
			continue
		}
		sb.WriteString(part.Text.Text)
	}

	return sb.String()
}

// countCitations 数知识库召回结果的来源条数，见 toolResultBlock 的说明。
func countCitations(text string) int {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.Contains(line, "#chunk-") {
			count++
		}
	}

	return count
}

func mediaBlock(kind, url, mimeType, filename string, block *schema.ContentBlock) Block {
	return Block{
		Type:     kind,
		URL:      url,
		MIMEType: mimeType,
		Filename: filename,
		index:    blockIndex(block),
	}
}

// blockIndex 取流式分片序号；非流式消息返回 -1（表示「不参与分片合并」）。
func blockIndex(block *schema.ContentBlock) int {
	if block == nil || block.StreamingMeta == nil {
		return -1
	}

	return block.StreamingMeta.Index
}

// clampText 按字节上限截断，且不会切碎一个多字节字符。
func clampText(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}

	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return text[:cut] + "…"
}

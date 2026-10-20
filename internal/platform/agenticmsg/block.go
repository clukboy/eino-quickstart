package agenticmsg

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
)

// 展示块类型。这些值是**对外契约**（REST 的 blocks[].type），改动等于改接口：
// 加了新类型前端会按「不认识的类型」跳过，删了或改名会让旧前端渲染不出来。
//
// 覆盖 eino 全部 ContentBlockType（20 种，见 agentic_message.go）：
//
//	user_input_text / assistant_gen_text  → text
//	reasoning                             → reasoning
//	function_tool_call / server_tool_call / mcp_tool_call → tool_call
//	function_tool_result / server_tool_result / mcp_tool_result → tool_result
//	tool_search_result                    → tool_search
//	mcp_list_tools_result                 → mcp_tools
//	mcp_tool_approval_request             → approval_request
//	mcp_tool_approval_response            → approval_response
//	user_input_* / assistant_gen_* 的图、音、视频、文件 → image / audio / video / file
//
// 新类型只出现在 blocks[].type 这个字符串里，字段仍是下面那几个 —— 没有为某个
// 类型单独加 DTO 字段，是因为「多一个字段」比「多一个类型值」对旧前端更危险。
const (
	BlockText             = "text"
	BlockReasoning        = "reasoning"
	BlockToolCall         = "tool_call"
	BlockToolResult       = "tool_result"
	BlockToolSearch       = "tool_search"
	BlockMCPTools         = "mcp_tools"
	BlockApprovalRequest  = "approval_request"
	BlockApprovalResponse = "approval_response"
	BlockImage            = "image"
	BlockAudio            = "audio"
	BlockVideo            = "video"
	BlockFile             = "file"
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
//   - reasoning 落，但按同一条上限截断（推理模型的思考过程常常比正文长数倍）。
type Block struct {
	Type string `json:"type"`

	// Text 是 text / reasoning 块的正文。
	Text string `json:"text,omitempty"`

	// Name 是工具类块的工具名（tool_call / tool_result / tool_search），
	// 或 mcp_tools 的服务标签。
	Name string `json:"name,omitempty"`

	// Arguments 是 tool_call / approval_request 的入参 JSON（按上限截断）。
	Arguments string `json:"arguments,omitempty"`

	// Summary 是结果类块的一句话摘要，例如「召回 3 条」「检索到 2 个来源」。
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
// maxBytes 限制两处长度：tool_call / approval_request 的入参、reasoning 的正文
// （<=0 表示不截断）。复用 Context.MaxToolOutputBytes 的用意是：模型看到的入参
// 本身就是截断过的，展示不该比模型看到的更完整。
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
			blocks = appendText(blocks, BlockText, block.UserInputText.Text, blockIndex(block))

		case block.AssistantGenText != nil:
			blocks = appendText(blocks, BlockText, block.AssistantGenText.Text, blockIndex(block))

		// 思考过程。单独一种块而不是并进 text：它不是回答，前端该能用别的方式
		// 折叠/弱化，REST 的 content 投影也不会把它算进正文。
		case block.Reasoning != nil:
			blocks = appendText(
				blocks, BlockReasoning,
				clampText(block.Reasoning.Text, maxBytes), blockIndex(block),
			)

		case block.FunctionToolCall != nil:
			blocks = append(blocks, Block{
				Type:      BlockToolCall,
				Name:      block.FunctionToolCall.Name,
				Arguments: clampText(block.FunctionToolCall.Arguments, maxBytes),
				index:     blockIndex(block),
			})

		case block.FunctionToolResult != nil:
			blocks = append(blocks, toolResultBlock(block.FunctionToolResult, blockIndex(block)))

		// 以下是**模型服务端执行**的工具（Responses 的 web_search / file_search /
		// code_interpreter / image_generation / shell）。它们与 FunctionToolCall
		// 的区别不是展示形态，而是「谁在执行」：这些调用不进 adk 的 ReAct 循环、
		// 不需要注册成 eino 工具、没有审批也没有断点，结果由模型服务端直接塞回
		// 上下文。对展示层来说仍是一次「调了工具、拿到了结果」，所以复用
		// tool_call / tool_result 两种块。
		case block.ServerToolCall != nil:
			blocks = append(blocks, Block{
				Type:      BlockToolCall,
				Name:      block.ServerToolCall.Name,
				Arguments: clampText(argumentsText(block.ServerToolCall.Arguments), maxBytes),
				index:     blockIndex(block),
			})

		case block.ServerToolResult != nil:
			blocks = append(blocks, Block{
				Type:    BlockToolResult,
				Name:    block.ServerToolResult.Name,
				Summary: serverToolSummary(block.ServerToolResult.Name, block.ServerToolResult.Content),
				index:   blockIndex(block),
			})

		// MCP 托管工具：调用/结果与上面同形，只是名字要带服务标签前缀。
		case block.MCPToolCall != nil:
			blocks = append(blocks, Block{
				Type:      BlockToolCall,
				Name:      mcpToolName(block.MCPToolCall.ServerLabel, block.MCPToolCall.Name),
				Arguments: clampText(block.MCPToolCall.Arguments, maxBytes),
				index:     blockIndex(block),
			})

		case block.MCPToolResult != nil:
			blocks = append(blocks, Block{
				Type:    BlockToolResult,
				Name:    mcpToolName(block.MCPToolResult.ServerLabel, block.MCPToolResult.Name),
				Summary: mcpResultSummary(block.MCPToolResult),
				index:   blockIndex(block),
			})

		// 工具检索（服务端 deferred tools）：只报「发现了几个」，不落工具定义 ——
		// 那是一整批 JSON Schema，落下来比正文还大。
		case block.ToolSearchFunctionToolResult != nil:
			blocks = append(blocks, Block{
				Type:    BlockToolSearch,
				Name:    block.ToolSearchFunctionToolResult.Name,
				Summary: toolSearchSummary(block.ToolSearchFunctionToolResult.Result),
				index:   blockIndex(block),
			})

		case block.MCPListToolsResult != nil:
			blocks = append(blocks, Block{
				Type:    BlockMCPTools,
				Name:    block.MCPListToolsResult.ServerLabel,
				Summary: mcpListSummary(block.MCPListToolsResult),
				index:   blockIndex(block),
			})

		case block.MCPToolApprovalRequest != nil:
			blocks = append(blocks, Block{
				Type:      BlockApprovalRequest,
				Name:      mcpToolName(block.MCPToolApprovalRequest.ServerLabel, block.MCPToolApprovalRequest.Name),
				Arguments: clampText(block.MCPToolApprovalRequest.Arguments, maxBytes),
				Summary:   "等待审批",
				index:     blockIndex(block),
			})

		case block.MCPToolApprovalResponse != nil:
			blocks = append(blocks, Block{
				Type:    BlockApprovalResponse,
				Summary: approvalResponseSummary(block.MCPToolApprovalResponse),
				index:   blockIndex(block),
			})

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

		default:
			// 兜底：eino 以后新增 block 类型时，这里不再把整条消息悄悄吞掉 ——
			// 至少把类型本身透出去。前端对不认识的 type 本来就是跳过。
			if block.Type != "" {
				blocks = append(blocks, Block{Type: string(block.Type), index: blockIndex(block)})
			}
		}
	}

	return blocks
}

// AppendText 把一段文本并进块序列（非流式路径用）。
//
// 紧挨着上一块的 text 会并进同一块，而不是各自成块 —— 流式回答是逐字到达的，
// 一个字一个块会让一条消息变成几百个 JSON 对象，前端也没法把它当成一段话排版。
func AppendText(blocks []Block, text string) []Block {
	return appendText(blocks, BlockText, text, -1)
}

// appendText 按「同类型 + 同分片序号」合并文本。
//
// 带上序号是必要的：一条消息里可以有多段 reasoning、也可以有多段正文（中间夹着
// 工具调用），只按类型合并会把不同分片揉成一段。
func appendText(blocks []Block, kind, text string, index int) []Block {
	if text == "" {
		return blocks
	}

	if n := len(blocks); n > 0 && blocks[n-1].Type == kind && blocks[n-1].index == index {
		blocks[n-1].Text += text
		return blocks
	}

	return append(blocks, Block{Type: kind, Text: text, index: index})
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
		// text / reasoning 是「越拼越长」的块，按序号并进上一块。
		if block.Type == BlockText || block.Type == BlockReasoning {
			blocks = appendText(blocks, block.Type, block.Text, block.index)
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

		case BlockReasoning,
			BlockToolCall, BlockToolResult, BlockToolSearch,
			BlockMCPTools, BlockApprovalRequest, BlockApprovalResponse:
			// 都不是「回答」：只有思考过程、或只有工具往返的一轮，仍按空轮次处理。
			// reasoning 尤其不能当正文 —— 那是模型的自言自语，不是给用户的答复。

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

// argumentsText 把服务端工具的入参投影成字符串。
//
// 这些工具的 Arguments 是 any（各家模型服务填的是自己的结构体），而不是像
// 函数工具那样的 JSON 字符串 —— 只能序列化一次。序列化失败不能影响展示：
// 入参本来就是可选信息，宁可空着也不要让整条消息投影不出来。
func argumentsText(arguments any) string {
	if arguments == nil {
		return ""
	}

	if text, ok := arguments.(string); ok {
		return text
	}

	raw, err := json.Marshal(arguments)
	if err != nil {
		return ""
	}

	return string(raw)
}

// serverToolSummary 给模型服务端工具的执行结果算一句摘要。
//
// 和函数工具一样，**正文不落库**：web_search 的 sources、file_search 的命中片段、
// code_interpreter 的输出都是模型服务端按本次请求现取的原文，落下来体积不可控，
// 而且下次会话里它已经是「没有出处的旧数据」。这里只留可数的量。
//
// 认字段靠的是 Responses 的返回结构（web_search.search.sources、
// file_search.results、code_interpreter.outputs）。结构变了不会报错，只会退化成
// 「已执行」—— 摘要本来就是尽力而为。
func serverToolSummary(name string, content any) string {
	if content == nil {
		return ""
	}

	payload, ok := asJSONObject(content)
	if !ok {
		return ""
	}

	// 结果按工具名分节，例如 {"web_search":{"search":{"sources":[...]}}}。
	section, _ := payload[name].(map[string]any)

	switch name {
	case "web_search":
		if n := countAt(section, "search", "sources"); n > 0 {
			return fmt.Sprintf("检索到 %d 个来源", n)
		}
		return "完成联网检索"

	case "file_search":
		if n := countAt(section, "results"); n > 0 {
			return fmt.Sprintf("命中 %d 个文件", n)
		}
		return "完成文件检索"

	case "code_interpreter":
		if n := countAt(section, "outputs"); n > 0 {
			return fmt.Sprintf("代码执行完成，%d 段输出", n)
		}
		return "代码执行完成"

	case "image_generation":
		return "已生成图片"

	case "shell":
		return "命令执行完成"

	default:
		return "已执行"
	}
}

// toolSearchSummary 给工具检索结果算摘要（服务端 deferred tools）。
func toolSearchSummary(result *schema.ToolSearchResult) string {
	if result == nil {
		return ""
	}

	if n := len(result.Tools); n > 0 {
		return fmt.Sprintf("发现 %d 个工具", n)
	}

	return "未发现工具"
}

// mcpListSummary 给 MCP 服务器的工具清单算摘要。
func mcpListSummary(result *schema.MCPListToolsResult) string {
	if result == nil {
		return ""
	}

	if result.Error != "" {
		return "列出工具失败"
	}

	return fmt.Sprintf("提供 %d 个工具", len(result.Tools))
}

// approvalResponseSummary 给审批结论算摘要。
func approvalResponseSummary(result *schema.MCPToolApprovalResponse) string {
	if result == nil {
		return ""
	}

	verdict := "已拒绝"
	if result.Approve {
		verdict = "已批准"
	}

	// 理由是人写的短句，截一段足够说明问题；完整内容不落库。
	if reason := strings.TrimSpace(result.Reason); reason != "" {
		return verdict + "：" + clampText(reason, 64)
	}

	return verdict
}

// mcpToolName 给 MCP 工具加服务标签前缀。
//
// 服务端托管的 MCP 允许挂多个 server，两个 server 各有一个叫 search 的工具，
// 只显示裸名字时前端与日志都分不清是哪一个。
func mcpToolName(serverLabel, name string) string {
	if serverLabel == "" {
		return name
	}

	return serverLabel + "/" + name
}

// mcpResultSummary 给 MCP 工具结果算摘要。
//
// Content 是一个 JSON 字符串（不保证结构），只量长度。
func mcpResultSummary(result *schema.MCPToolResult) string {
	if result == nil {
		return ""
	}

	if result.Error != nil {
		return "执行失败"
	}

	if result.Content == "" {
		return ""
	}

	return fmt.Sprintf("返回 %d 字符", utf8.RuneCountInString(result.Content))
}

// asJSONObject 把任意结构体/字符串解码成 map。
//
// 先序列化再反序列化，是为了不依赖具体组件的 Go 类型 —— 展示层不该为了读一个
// 计数字段去 import 某个模型实现。
func asJSONObject(value any) (map[string]any, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false
	}

	return payload, true
}

// countAt 沿路径取一个数组并返回长度；路径断了返回 0。
func countAt(payload map[string]any, path ...string) int {
	var current any = payload

	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return 0
		}

		current, ok = object[key]
		if !ok {
			return 0
		}
	}

	items, ok := current.([]any)
	if !ok {
		return 0
	}

	return len(items)
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

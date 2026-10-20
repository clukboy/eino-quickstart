package chat

// Kind 是推给调用方的事件类型。取值就是对外 SSE 帧的 type，传输层直接透传，
// 不做二次映射 —— 多一层映射只会多一处可以写错的地方。
type Kind string

const (
	// KindMessage 是模型产出的正文增量。多次到达的内容按顺序拼接即完整回答。
	KindMessage Kind = "message"
	// KindTool 是工具活动。Tool 是工具名，Summary 是人话摘要（「召回 3 条」）。
	// 它只是「过程可见」，不携带工具返回的原文 —— 那是按当时的授权召回的，
	// 推到前端等于把已撤销的数据集内容送出去。
	KindTool Kind = "tool"
	// KindError 表示这一轮失败。此事件之后不会再有 message / tool / done。
	KindError Kind = "error"
	// KindDone 表示这一轮正常收尾，回答已经落库。
	KindDone Kind = "done"
)

// Event 是一次对话推进过程中推给调用方的最小事实。
//
// 字段刻意稀疏（一帧只带该类型用得上的那几个），与 SSE 契约一致：
// 传输层可以直接把它序列化成帧，不需要判断「这个字段现在有没有意义」。
type Event struct {
	Kind      Kind
	SessionID string
	Content   string
	Tool      string
	Summary   string
	Err       string
}

// Request 是一轮对话的输入。
//
// Subject 来自认证身份，**不能**由请求体传入 —— 会话归属、知识库授权都按它
// 解析，让它变成参数就等于让调用方自己声明自己是谁。
type Request struct {
	Subject   string
	SessionID string
	Message   string
}

// Sink 接收事件，返回 false 表示调用方已经停止接收（客户端断开）。
type Sink func(Event) bool

// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package stream

import (
	"context"
	"strings"

	"eino-quickstart/internal/application/chat"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ChatLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewChatLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ChatLogic {
	return &ChatLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// Chat 是 SSE 端点与 chat 应用层之间的唯一适配器。
//
// 它只做三件事：认证、请求校验、把 chat.Event 变成帧。会话、历史、轮次、
// 失败收尾全部在 application/chat 里 —— 这一层不该知道「turn」是什么。
//
// 两条必须守住的约定：
//
//  1. **失败要先发 error 帧再返回 error。** goctl 生成的 SSE handler 只把返回的
//     error 记日志、不写响应，所以每个提前返回的路径都要自己推一帧，否则客户端
//     只会看到连接静默关闭。
//  2. **sink 返回 false 就停。** 那表示请求 context 已结束（客户端断开），
//     继续迭代只是在给没人读的流烧 token。
func (l *ChatLogic) Chat(req *types.ChatReq, client chan<- *types.ChatEvent) error {
	identity, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return emitError(l.ctx, client, "", "unauthorized")
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		return emitError(l.ctx, client, req.SessionID, "message is empty")
	}

	// 这里返回的 error 只进日志：客户端看到的是已经推出去的那条 error 帧。
	return l.svcCtx.Chat.Stream(
		l.ctx,
		chat.Request{
			Subject:   identity.Subject,
			SessionID: req.SessionID,
			Message:   message,
		},
		func(event chat.Event) bool {
			return emit(l.ctx, client, toChatEvent(event))
		},
	)
}

// toChatEvent 把 chat.Event 拍平成线上帧。
//
// types.ChatEvent 是 goctl 从 stream.api 生成的，字段是 type / session_id /
// agent / content / error / approval_id —— **没有**给工具事件留独立字段。
// 所以 tool 帧把「工具名：摘要」写进 content：客户端本来就该按 type 分派，
// 只有 type=message 的 content 才拼进回答正文，工具帧不会被误当正文。
//
// 想给工具帧单独的结构化字段，就必须改 stream.api 再生成（会连带改 swagger
// 契约），那是另一次改动；这一层只负责在当前契约里不丢信息。
func toChatEvent(event chat.Event) *types.ChatEvent {
	content := event.Content
	if event.Kind == chat.KindTool {
		content = toolNote(event.Tool, event.Summary)
	}

	return &types.ChatEvent{
		Type:      string(event.Kind),
		SessionID: event.SessionID,
		Content:   content,
		Error:     event.Err,
	}
}

// toolNote 拼工具帧的人话内容，例如 `web_search：召回 3 条`。
//
// 任何一半缺失都不留分隔符：只拿到工具名就只发工具名，只拿到摘要就只发摘要。
func toolNote(name, summary string) string {
	switch {
	case name == "":
		return summary
	case summary == "":
		return name
	default:
		return name + "：" + summary
	}
}

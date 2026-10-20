// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package stream

import (
	"context"
	"errors"

	"eino-quickstart/internal/transport/restapi/internal/types"
)

// eventError 是唯一由传输层自己产生的事件类型：认证失败、请求非法这类
// 「还没进到应用层就失败」的情况。
//
// message / tool / done 三种不在这里定义 —— 它们的值是 chat.Kind，直接透传。
// 在传输层再抄一份常量，等于让同一个字符串有两个来源，改一处就会漏一处。
const eventError = "error"

// emit 把一帧交给 handler 写出。
//
// 返回 false 表示请求 context 已结束（客户端断开），调用方应当立刻停手：继续跑
// Agent 只会白烧 token，再往 channel 里写也没人读了。
func emit(ctx context.Context, client chan<- *types.ChatEvent, event *types.ChatEvent) bool {
	select {
	case client <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

// emitError 推一条 error 帧，并返回一个可供日志的错误。
//
// 为什么错误帧必须由 logic 自己写：goctl 生成的 SSE handler 只把 logic 返回的
// error 记进日志、**不写响应**（sse_handler.tpl 里 `l.Chat(...)` 那段只调
// logc.Errorw）。少了一帧，客户端只会看到连接静默关闭，分不清「网络断了」还是
// 「服务端拒绝了」。
//
// message 是给客户端看的稳定文案，别把底层错误拼进去 —— 那些进日志。
func emitError(ctx context.Context, client chan<- *types.ChatEvent, sessionID, message string) error {
	emit(ctx, client, &types.ChatEvent{Type: eventError, SessionID: sessionID, Error: message})
	return errors.New(message)
}

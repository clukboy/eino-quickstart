// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package stream

import (
	"context"
	"errors"
	"fmt"

	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/cloudwego/eino/adk"
)

// 流上事件的四种 type，与 docs/stream/stream.api 的 ChatEvent 一一对应。
// 客户端按 payload 的 type 分派（生成器不写 `event:` 行，见该文件的说明）。
const (
	eventMessage          = "message"
	eventError            = "error"
	eventDone             = "done"
	eventApprovalRequired = "approval_required"
)

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

// fail 是 emitError 的带因版：帧上给稳定文案，返回的 error 带上底层原因。
func fail(
	ctx context.Context,
	client chan<- *types.ChatEvent,
	sessionID, message string,
	cause error,
) error {
	emit(ctx, client, &types.ChatEvent{Type: eventError, SessionID: sessionID, Error: message})
	return fmt.Errorf("%s: %w", message, cause)
}

// approvalIDFromInterrupt 从中断数据里取审批单 id（由审批工具在 interrupt 时写入）。
func approvalIDFromInterrupt(data any) (string, bool) {
	values, ok := data.(map[string]string)
	if !ok {
		return "", false
	}
	approvalID := values["approval_id"]
	return approvalID, approvalID != ""
}

// rootCauseInterruptID 返回根因中断上下文的 id —— 恢复执行时它就是
// adk.ResumeParams.Targets 的 key。
func rootCauseInterruptID(contexts []*adk.InterruptCtx) (string, bool) {
	for _, item := range contexts {
		if item.IsRootCause && item.ID != "" {
			return item.ID, true
		}
	}
	return "", false
}

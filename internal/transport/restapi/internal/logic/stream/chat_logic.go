// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package stream

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/application/middleware"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/cloudwego/eino/adk"
	"github.com/google/uuid"
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

// Chat 跑一轮多 agent 对话，把过程推成 SSE 帧。
//
// 帧格式、四种事件的含义、判错口径都在 docs/stream/stream.api；这里只记三条
// 实现约定：
//
//  1. **失败必须先发 error 帧再返回 error。** 生成的 handler 只把返回的 error
//     记日志、不写响应，所以每个提前返回的路径都要自己把原因推给客户端
//     （emitError / fail）。漏一处的表现是客户端「连接莫名关闭、没有任何提示」。
//  2. **客户端断开就立刻收工。** emit 返回 false 表示请求 context 已结束，此时
//     直接 return nil —— 继续迭代 Agent 只是在给一个没人收的流烧 token。返回
//     nil 而非 error，是因为这不是服务端故障，不该进错误日志。
//  3. **一轮对话在库里是三样东西**：turn（用户消息 + 最终回答）、run（本次执行
//     的租约与状态）、被打断时的审批单。顺序上 turn 先落、run 后建 —— run 的
//     租约要求对应的 turn 已经存在。
func (l *ChatLogic) Chat(req *types.ChatReq, client chan<- *types.ChatEvent) error {
	identity, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return emitError(l.ctx, client, "", "unauthorized")
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		return emitError(l.ctx, client, req.SessionID, "message is empty")
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	// 会话不存在就地建一个，而不是报错。
	//
	// 正常流程是客户端先 POST /api/v1/sessions 拿到 id 再对话，但那条路会漏：
	// 客户端把 id 丢了（刷新、换标签页、本地存储被清）之后只会带着一个陌生 uuid
	// 回来，而 History 与 Turns.Start 都会因为「查不到」把这条流掐断 —— 用户
	// 看到的是「一发消息就失败」，且没有任何自愈路径。
	//
	// 这不等于放开越权：GetOrCreate 先按 subject 查，命中不了才建，所以带别人的
	// session_id 仍然会被拒（那是走到 getOwned 的越权分支，返回同一个「查不到」）。
	if err := l.svcCtx.Sessions.GetOrCreate(l.ctx, sessionID, identity.Subject); err != nil {
		return fail(l.ctx, client, sessionID, "open session failed", err)
	}

	history, err := l.svcCtx.Sessions.History(l.ctx, sessionID, identity.Subject)
	if err != nil {
		return fail(l.ctx, client, sessionID, "load session history failed", err)
	}

	turnID := uuid.NewString()
	if _, err := l.svcCtx.Turns.Start(l.ctx, turnID, sessionID, identity.Subject, message); err != nil {
		return fail(l.ctx, client, sessionID, "create chat turn failed", err)
	}

	// 会话标题取首条用户消息（SetTitleIfEmpty 只在标题还是空的时候写）。
	//
	// 失败只记日志、不掐流：一句话的标题写不进去不该让整轮对话失败，退化的结果
	// 只是那条会话在侧栏里继续显示「新对话」。
	if err := l.svcCtx.Sessions.SetTitleIfEmpty(
		l.ctx, sessionID, identity.Subject, session.DeriveTitle(message),
	); err != nil {
		l.Errorf("set session title failed: %v", err)
	}

	history = append(history, agent.UserMessage(message))
	history = l.svcCtx.Agent.Context.Prepare(history)

	runID := uuid.NewString()
	if err := l.svcCtx.Runs.Create(
		l.ctx,
		runID,
		sessionID,
		identity.Subject,
		message,
		time.Now().UTC().Add(24*time.Hour),
	); err != nil {
		return fail(l.ctx, client, sessionID, "create agent run failed", err)
	}

	// session / turn 通过 context 往下传：工具与检索要按会话做授权与审计，而它们
	// 只拿得到 context。runCtx 同时是 Agent 的执行 context —— 客户端断开时它随
	// 请求一起取消，正在跑的这一轮就停了。
	runCtx := middleware.WithSession(l.ctx, sessionID)
	runCtx = middleware.WithTurn(runCtx, turnID)

	checkpointID := runID
	iter := l.svcCtx.Agent.Runner.Run(
		runCtx,
		history,
		adk.WithCheckPointID(checkpointID),
		adk.WithSessionValues(map[string]any{
			"session_id": sessionID,
			"run_id":     runID,
			"turn_id":    turnID,
		}),
	)

	var assistant strings.Builder

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		// 审批拦下这轮：落 checkpoint、把 turn 与 run 标成中断，然后让客户端去
		// 决策。这条流的使命到此为止，后续由 resume 那条接上。
		if event.Action != nil && event.Action.Interrupted != nil {
			return l.handleInterrupt(runCtx, client, event, sessionID, turnID, identity.Subject, runID, checkpointID)
		}

		// 单个 event 的错误不终止整条流：Agent 可能已经产出过内容，把原因推给
		// 客户端之后继续读，后面还可能有正常输出。
		if event.Err != nil {
			emit(runCtx, client, &types.ChatEvent{
				Type:      eventError,
				SessionID: sessionID,
				Error:     event.Err.Error(),
			})
		}

		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		output := event.Output.MessageOutput
		if output.IsStreaming {
			if !streamMessage(runCtx, client, &assistant, sessionID, event.AgentName, output) {
				return nil
			}
			continue
		}

		if output.Message != nil && output.Message.Content != "" {
			assistant.WriteString(output.Message.Content)
			if !emit(runCtx, client, &types.ChatEvent{
				Type:      eventMessage,
				SessionID: sessionID,
				Agent:     event.AgentName,
				Content:   output.Message.Content,
			}) {
				return nil
			}
		}
	}

	// 收尾：assistant 全文落库，再发 done。done 之后不再有帧 —— 客户端靠它判断
	// 「这次回答是完整的」，没有 done 就说明中途断了。
	if err := l.svcCtx.Turns.Complete(l.ctx, turnID, identity.Subject, assistant.String()); err != nil {
		return fail(l.ctx, client, sessionID, "complete chat turn failed", err)
	}

	emit(l.ctx, client, &types.ChatEvent{Type: eventDone, SessionID: sessionID})
	return nil
}

// streamMessage 转发一个流式输出，返回 false 表示客户端已断开。
//
// 增量直接拼进 assistant：一次答复可能跨多个 event（多 agent 编排下各 agent
// 各说一段），最终落库的是全文，所以累积要跨 event —— 这也是 assistant 由调用
// 方持有、而不是在这里新建的原因。
func streamMessage(
	ctx context.Context,
	client chan<- *types.ChatEvent,
	assistant *strings.Builder,
	sessionID, agentName string,
	output *adk.MessageVariant,
) bool {
	for {
		message, err := output.MessageStream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// 读流失败只发一帧、不终止：已经拼出来的内容仍然有效，让客户端自己
			// 决定是重试还是展示半截回答。
			emit(ctx, client, &types.ChatEvent{
				Type:      eventError,
				SessionID: sessionID,
				Error:     err.Error(),
			})
			continue
		}
		if message.Content == "" {
			continue
		}
		assistant.WriteString(message.Content)
		if !emit(ctx, client, &types.ChatEvent{
			Type:      eventMessage,
			SessionID: sessionID,
			Agent:     agentName,
			Content:   message.Content,
		}) {
			return false
		}
	}
	return true
}

// handleInterrupt 保存审批断点并告知客户端去决策。
//
// 三处状态都要落：审批单挂上 checkpoint 与中断 id（否则 resume 找不到续跑入口）、
// turn 与 run 标成中断。任一步失败都要发 error 帧 —— 「客户端以为在等审批，库里
// 却没有可续跑的断点」是最难查的一种状态。
func (l *ChatLogic) handleInterrupt(
	ctx context.Context,
	client chan<- *types.ChatEvent,
	event *adk.AgentEvent,
	sessionID, turnID, subject, runID, checkpointID string,
) error {
	approvalID, ok := approvalIDFromInterrupt(event.Action.Interrupted.Data)
	if !ok {
		return emitError(ctx, client, sessionID, "approval interrupt did not include an approval ID")
	}

	interruptID, ok := rootCauseInterruptID(event.Action.Interrupted.InterruptContexts)
	if !ok {
		return emitError(ctx, client, sessionID, "approval interrupt did not include a root cause")
	}

	if err := l.svcCtx.Approvals.AttachCheckpoint(ctx, approvalID, runID, checkpointID, interruptID); err != nil {
		_ = l.svcCtx.Runs.MarkFailed(ctx, runID, "approval_checkpoint_attach_failed")
		return fail(ctx, client, sessionID, "save approval checkpoint failed", err)
	}

	if err := l.svcCtx.Turns.MarkInterrupted(ctx, turnID, subject, approvalID, checkpointID); err != nil {
		return fail(ctx, client, sessionID, "mark chat turn interrupted failed", err)
	}

	if err := l.svcCtx.Runs.MarkInterrupted(ctx, runID, approvalID); err != nil {
		return fail(ctx, client, sessionID, "mark interrupted run failed", err)
	}

	emit(ctx, client, &types.ChatEvent{
		Type:       eventApprovalRequired,
		SessionID:  sessionID,
		ApprovalID: approvalID,
	})
	return nil
}

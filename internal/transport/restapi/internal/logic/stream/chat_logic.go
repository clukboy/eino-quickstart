// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package stream

import (
	"context"
	"errors"
	"strings"
	"time"

	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/application/middleware"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
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

	round, clientGone, _ := forwardRound(runCtx, client, sessionID, iter, false)
	if clientGone {
		return nil
	}

	if round.Interrupted != nil {
		return l.handleInterrupt(runCtx, client, round.Interrupted, sessionID, turnID, identity.Subject, runID, checkpointID)
	}

	// event 级错误不掐流，也不单独发帧 —— 只要这一轮最终有正文，它就是可用的回答；
	// 没有正文会走下面的兜底，兜底再失败才发 error。所以这里只落日志，留可查的现场。
	// routed 那行是路由准确率的唯一离线来源，单独记。
	for _, eventErr := range round.Errors {
		l.Errorf("chat turn event error: session=%s turn=%s err=%v", sessionID, turnID, eventErr)
	}
	l.Infof("chat turn routed: session=%s turn=%s agent=%s", sessionID, turnID, routingLabel(round.RoutedTo))

	answer := round.Assistant

	var fallbackErr error

	if strings.TrimSpace(answer) == "" {
		answer, clientGone, fallbackErr = l.runGeneralFallback(runCtx, client, sessionID, history)
		if clientGone {
			return nil
		}
	}

	if err := l.svcCtx.Turns.Complete(l.ctx, turnID, identity.Subject, answer); err != nil {
		return fail(l.ctx, client, sessionID, "complete chat turn failed", err)
	}

	// 兜底也失败：发 error 帧并结束，**不发 done**。stream.api 的契约是「error
	// 表示本次回答不完整」，再补一个 done 会让客户端把一次失败判成成功。
	if fallbackErr != nil {
		return fail(l.ctx, client, sessionID, "general fallback failed", fallbackErr)
	}

	emit(l.ctx, client, &types.ChatEvent{Type: eventDone, SessionID: sessionID})
	return nil
}

// runGeneralFallback 在 root 整轮没有产出正文时，用 general_agent 单独补跑一次。
//
// 三条约束：
//  1. 走独立的 GeneralRunner（不挂 checkpoint store），不碰本轮 runID 的断点；
//  2. 输入是已经裁剪过的 history，而不是重新拼一串 prompt —— 追问也要看得见上下文；
//  3. 不传 session values：那会让 Eino 用 prompt.FString 格式化 instruction，兜底
//     路径没必要为这个开口子。
//
// 返回的 error 表示兜底也没能产出内容。它**不自己发帧**：调用方会在落库之后用
// fail 一次性发出 error 帧并结束流，避免出现「error 之后又 done」的矛盾序列。
func (l *ChatLogic) runGeneralFallback(
	ctx context.Context,
	client chan<- *types.ChatEvent,
	sessionID string,
	history []*schema.Message,
) (string, bool, error) {
	if l.svcCtx.Agent == nil || l.svcCtx.Agent.GeneralRunner == nil {
		return "", false, errors.New("general runner is not configured")
	}

	iter := l.svcCtx.Agent.GeneralRunner.Run(ctx, history)

	round, clientGone, fatal := forwardRound(ctx, client, sessionID, iter, true)
	if clientGone {
		return "", true, nil
	}
	if fatal != nil {
		l.Errorf("general fallback failed: session=%s err=%v", sessionID, fatal)
	}

	if round.Assistant != "" {
		l.Infof("general fallback answered: session=%s", sessionID)
		return round.Assistant, false, nil
	}

	// 刻意不回落本地主模型：兜底专员的模型配错属于部署问题，应当被看见，
	// 而不是被一次静默的降级掩盖掉。
	l.Errorf("general fallback produced no content: session=%s", sessionID)
	if fatal != nil {
		return "", false, fatal
	}
	return "", false, errors.New("general agent produced no content")
}

// handleInterrupt 保存审批断点并告知客户端去决策。
//
// 三处状态都要落：审批单挂上 checkpoint 与中断 id（否则 resume 找不到续跑入口）、
// turn 与 run 标成中断。任一步失败都要发 error 帧 —— 「客户端以为在等审批，库里
// 却没有可续跑的断点」是最难查的一种状态。
func (l *ChatLogic) handleInterrupt(ctx context.Context, client chan<- *types.ChatEvent, event *adk.AgentEvent, sessionID, turnID, subject, runID, checkpointID string) error {
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

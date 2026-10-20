// Package chat 是一轮对话的完整编排。
//
// 它存在的理由：这些规则原先散在传输层的 ChatLogic 里 —— 认证之后紧接着
// 建会话、读历史、落 turn、跑 Agent、落回答、改状态，八件事挤在一个函数里，
// 而且每一件都直接操作 store。结果是「一轮对话是什么」这件事只能去读 SSE
// handler 才能知道，换个入口（比如以后的 IM 机器人）就得把这段逻辑抄一遍。
//
// 现在边界很清楚：
//
//	传输层  认证 + 把 Event 写成 SSE 帧（它不知道 turn、run、history 是什么）
//	本包    一轮对话的全部业务规则（它不知道 SSE、HTTP 存在）
//	agent   只负责「给定消息列表，产出事件流」
//
// 失败时的约定与 SSE 契约一致：**先往 sink 推一条 KindError，再返回 error**。
// 返回的 error 只用于日志，客户端看到的是 Event.Err 里那句稳定文案 ——
// 底层原因（SQL 错误、模型报错原文）不能外泄。
package chat

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/platform/agenticmsg"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/platform/persistence/turn"

	"github.com/cloudwego/eino/adk"
	"github.com/google/uuid"
)

// errEmptyAnswer 表示这一轮跑完了但什么都没产出（连占位正文都算不出来）。
var errEmptyAnswer = errors.New("agent produced no content")

// Service 编排一轮对话：会话 → 历史 → 轮次 → Agent → 落库。
type Service struct {
	Agent    *agent.Harness
	Sessions *session.Store
	Turns    *turn.Store
	Logger   *slog.Logger
}

func NewService(
	harness *agent.Harness,
	sessions *session.Store,
	turns *turn.Store,
	logger *slog.Logger,
) *Service {
	return &Service{Agent: harness, Sessions: sessions, Turns: turns, Logger: logger}
}

// Stream 跑一轮对话，把过程以 Event 推给 sink。
//
// sink 返回 false 表示调用方已经不收了（客户端断开）。此时立刻停止迭代 ——
// 继续跑只是在给一条没人读的流烧 token —— 并且把 turn 收成 FAILED，
// 否则它会永远停在 RUNNING：maintenance 只清理终态轮次，卡住的会一直留着。
func (s *Service) Stream(ctx context.Context, req Request, sink Sink) error {
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	if err := s.Sessions.GetOrCreate(ctx, sessionID, req.Subject); err != nil {
		return fail(sink, sessionID, "open session failed", err)
	}

	history, err := s.Sessions.History(ctx, sessionID, req.Subject)
	if err != nil {
		return fail(sink, sessionID, "load session history failed", err)
	}

	turnID := uuid.NewString()
	if _, err := s.Turns.Start(ctx, turnID, sessionID, req.Subject, req.Message); err != nil {
		return fail(sink, sessionID, "create chat turn failed", err)
	}

	if err := s.Sessions.SetTitleIfEmpty(
		ctx, sessionID, req.Subject, session.DeriveTitle(req.Message),
	); err != nil {
		s.Logger.Error("set session title failed", slog.String("session_id", sessionID), slog.Any("error", err))
	}

	history = append(history, agent.UserMessage(req.Message))
	history = s.Agent.Context.Prepare(history)

	runOpts := []adk.AgentRunOption{
		adk.WithSessionValues(map[string]any{
			"session_id": sessionID,
			"turn_id":    turnID,
		}),
	}

	if len(s.Agent.ModelOptions) > 0 {
		runOpts = append(runOpts, adk.WithChatModelOptions(s.Agent.ModelOptions))
	}

	iter := s.Agent.Runner.Run(ctx, history, runOpts...)

	round, clientGone := pump(ctx, iter, sessionID, s.Agent.Context.MaxToolOutputBytes, sink)
	if clientGone {
		
		_ = s.Turns.Fail(context.WithoutCancel(ctx), turnID, req.Subject)
		return nil
	}

	// 事件级错误不掐流：只要这一轮最终有正文，它就是可用的回答。
	for _, roundErr := range round.Errors {
		s.Logger.Error(
			"chat round event error",
			slog.String("session_id", sessionID),
			slog.String("turn_id", turnID),
			slog.Any("error", roundErr),
		)
	}

	answer := round.Assistant
	if strings.TrimSpace(answer) == "" {
		// 只有非文本产物的一轮（模型只回了图）也必须落库：正文用一句占位顶上，
		// 内容本体在 blocks 里。否则 content 为空、session_messages 不写，
		// 这一轮就从历史里彻底消失 —— 用户那句还在、回答没了。
		answer = agenticmsg.FallbackText(round.Blocks)
	}
	if strings.TrimSpace(answer) == "" {
		_ = s.Turns.Fail(ctx, turnID, req.Subject)
		return fail(sink, sessionID, "agent produced no content", errEmptyAnswer)
	}

	if err := s.Turns.Complete(ctx, turnID, req.Subject, answer, round.Blocks); err != nil {
		_ = s.Turns.Fail(ctx, turnID, req.Subject)
		return fail(sink, sessionID, "complete chat turn failed", err)
	}

	sink(Event{Kind: KindDone, SessionID: sessionID})
	return nil
}

// fail 推一条 error 事件并返回带底层原因的错误（原因只进日志）。
func fail(sink Sink, sessionID, message string, cause error) error {
	sink(Event{Kind: KindError, SessionID: sessionID, Err: message})
	return errors.Join(errors.New(message), cause)
}

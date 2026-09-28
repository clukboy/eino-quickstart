// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package stream

import (
	"context"
	"errors"
	"io"
	"strings"

	"eino-quickstart/internal/application/middleware"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/cloudwego/eino/adk"
	"github.com/zeromicro/go-zero/core/logx"
)

type ResumeApprovalLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewResumeApprovalLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResumeApprovalLogic {
	return &ResumeApprovalLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ResumeApproval 从审批断点续跑被拦下的那轮对话，继续往同一条格式的流上发帧。
//
// 与 Chat 的关键差别是「这条流的所有权已经被抢占过」：
//
//   - ClaimForResume 把审批单从 APPROVED 推到 RESUMING，同一张单只允许一条流
//     续跑（幂等保护）。所以**接手之后的每条退出路径都要想清楚该不该 Release**：
//     放弃续跑就 ReleaseResume 退回 APPROVED 让客户端能重试；正常跑完不 Release，
//     审批单的终态由工具重新执行时的 Consume 决定。
//   - 抢单失败的原因（已被别的流抢占、还没批、已过期、没有断点）都直接透给客户端。
//     这些是它需要知道的、也是它唯一能自救的信息；原因文案来自 store，本身不含
//     内部细节。
func (l *ResumeApprovalLogic) ResumeApproval(
	req *types.ResumeApprovalPathReq,
	client chan<- *types.ChatEvent,
) error {
	identity, ok := auth.IdentityFromContext(l.ctx)
	if !ok {
		return emitError(l.ctx, client, "", "unauthorized")
	}

	approvalID := strings.TrimSpace(req.ID)
	if approvalID == "" {
		return emitError(l.ctx, client, "", "approval id is required")
	}

	record, err := l.svcCtx.Approvals.ClaimForResume(l.ctx, approvalID, identity.Subject)
	if err != nil {
		return emitError(l.ctx, client, "", err.Error())
	}

	runCtx := middleware.WithSession(l.ctx, record.SessionID)

	// ClaimForResume 只在有断点时才放行，这里是防御性兜底：宁可退回 APPROVED
	// 让客户端重试，也不要拿一个 nil checkpoint 去 Resume。
	if record.CheckpointID == nil || record.InterruptID == nil {
		_ = l.svcCtx.Approvals.ReleaseResume(runCtx, record.ID)
		return emitError(runCtx, client, record.SessionID, "approval is missing its checkpoint")
	}

	iter, err := l.svcCtx.Agent.Runner.ResumeWithParams(
		runCtx,
		*record.CheckpointID,
		&adk.ResumeParams{
			Targets: map[string]any{
				*record.InterruptID: approvalID,
			},
		},
		adk.WithSessionValues(map[string]any{
			"session_id": record.SessionID,
		}),
	)
	if err != nil {
		_ = l.svcCtx.Approvals.ResetResuming(runCtx, approvalID)
		return fail(l.ctx, client, record.SessionID, "resume approval failed", err)
	}

	return l.streamResume(runCtx, client, record, iter)
}

// streamResume 转发续跑的事件流，并在收尾时把 turn 补完。
//
// 与 Chat 里的转发有一处刻意的不同：这里一遇到错误就 ReleaseResume 并结束，
// 不像 Chat 那样「发一帧 error 继续读」。续跑是接在审批之后的，出错的流没有再
// 继续的语义；退回 APPROVED 让用户重试比拖着一条坏流更干净。
func (l *ResumeApprovalLogic) streamResume(
	ctx context.Context,
	client chan<- *types.ChatEvent,
	record *approval.Request,
	iter *adk.AsyncIterator[*adk.AgentEvent],
) error {
	var assistant strings.Builder

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			_ = l.svcCtx.Approvals.ReleaseResume(ctx, record.ID)
			return fail(ctx, client, record.SessionID, "resume stream failed", event.Err)
		}

		// 续跑里又撞上审批：当前实现不支持二次中断（没有把新断点接回去的链路），
		// 所以退回 APPROVED 并明确告诉客户端，而不是留下一条含糊的死流。
		if event.Action != nil && event.Action.Interrupted != nil {
			_ = l.svcCtx.Approvals.ReleaseResume(ctx, record.ID)
			return emitError(ctx, client, record.SessionID, "resumed execution requires another approval")
		}

		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}

		output := event.Output.MessageOutput

		if output.IsStreaming {
			for {
				message, err := output.MessageStream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					_ = l.svcCtx.Approvals.ReleaseResume(ctx, record.ID)
					return fail(ctx, client, record.SessionID, "resume stream failed", err)
				}
				if message.Content == "" {
					continue
				}
				assistant.WriteString(message.Content)
				if !emit(ctx, client, &types.ChatEvent{
					Type:      eventMessage,
					SessionID: record.SessionID,
					Agent:     event.AgentName,
					Content:   message.Content,
				}) {
					// 客户端断开：这轮续跑没了接收方，把审批单退回 APPROVED，
					// 让它能被重新发起（旧实现这里是直接 return，审批单会一直
					// 卡在 RESUMING 直到过期）。
					_ = l.svcCtx.Approvals.ReleaseResume(ctx, record.ID)
					return nil
				}
			}
			continue
		}

		if output.Message != nil && output.Message.Content != "" {
			assistant.WriteString(output.Message.Content)
			if !emit(ctx, client, &types.ChatEvent{
				Type:      eventMessage,
				SessionID: record.SessionID,
				Agent:     event.AgentName,
				Content:   output.Message.Content,
			}) {
				_ = l.svcCtx.Approvals.ReleaseResume(ctx, record.ID)
				return nil
			}
		}
	}

	if record.TurnID == nil {
		_ = l.svcCtx.Approvals.ReleaseResume(ctx, record.ID)
		return emitError(ctx, client, record.SessionID, "approval is missing its chat turn")
	}

	if err := l.svcCtx.Turns.Complete(ctx, *record.TurnID, record.RequestedBy, assistant.String()); err != nil {
		_ = l.svcCtx.Approvals.ReleaseResume(ctx, record.ID)
		return fail(ctx, client, record.SessionID, "complete resumed chat turn failed", err)
	}

	emit(ctx, client, &types.ChatEvent{Type: eventDone, SessionID: record.SessionID})
	return nil
}

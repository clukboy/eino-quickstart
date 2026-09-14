package restapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/application/middleware"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"

	"github.com/cloudwego/eino/adk"
	"github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

// chatRequest is the body of a chat request. It lives here rather than in the
// generated types package because POST /api/v1/chat is registered by hand (see
// the package comment): goctl's SSE generator cannot emit the named frames the
// client contract requires.
type chatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// streamHandlers holds the two SSE endpoints. They share the same dependency set
// as the generated logic through ServiceContext.
type streamHandlers struct {
	svc *svc.ServiceContext
}

func newStreamHandlers(serverCtx *svc.ServiceContext) *streamHandlers {
	return &streamHandlers{svc: serverCtx}
}

// Chat streams a multi-agent answer as Server-Sent Events. All pre-flight work
// (history lookup, turn/run creation) happens before the stream is opened so
// failures return a normal JSON error instead of a half-open stream.
func (h *streamHandlers) Chat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	identity, ok := auth.IdentityFromContext(ctx)
	if !ok {
		httpx.Fail(ctx, w, httpx.Unauthorized("unauthorized"))
		return
	}

	request, err := decodeChatRequest(r)
	if err != nil {
		httpx.Fail(ctx, w, httpx.BadRequest("invalid JSON request: "+err.Error()))
		return
	}

	request.Message = strings.TrimSpace(request.Message)
	if request.Message == "" {
		httpx.Fail(ctx, w, httpx.BadRequest("message is empty"))
		return
	}
	if request.SessionID == "" {
		request.SessionID = uuid.NewString()
	}

	history, err := h.svc.Sessions.History(ctx, request.SessionID, identity.Subject)
	if err != nil {
		httpx.Fail(ctx, w, httpx.Internal("load session history failed"))
		return
	}

	turnID := uuid.NewString()
	if _, err := h.svc.Turns.Start(ctx, turnID, request.SessionID, identity.Subject, request.Message); err != nil {
		httpx.Fail(ctx, w, httpx.Internal("create chat turn failed"))
		return
	}

	history = append(history, agent.UserMessage(request.Message))
	history = h.svc.Agent.Context.Prepare(history)

	runID := uuid.NewString()
	if err := h.svc.Runs.Create(
		ctx,
		runID,
		request.SessionID,
		identity.Subject,
		request.Message,
		time.Now().UTC().Add(24*time.Hour),
	); err != nil {
		httpx.Fail(ctx, w, httpx.Internal("create agent run failed"))
		return
	}

	stream, err := httpx.NewWriter(w)
	if err != nil {
		httpx.Fail(ctx, w, httpx.Internal("streaming is unavailable"))
		return
	}
	defer func() { _ = stream.Close() }()

	runCtx := middleware.WithSession(ctx, request.SessionID)
	runCtx = middleware.WithTurn(runCtx, turnID)

	checkpointID := runID
	iter := h.svc.Agent.Runner.Run(
		runCtx,
		history,
		adk.WithCheckPointID(checkpointID),
		adk.WithSessionValues(map[string]any{
			"session_id": request.SessionID,
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

		if event.Action != nil && event.Action.Interrupted != nil {
			h.handleInterrupt(runCtx, stream, event, request.SessionID, turnID, identity.Subject, runID, checkpointID)
			return
		}

		if event.Err != nil {
			_ = stream.Send(httpx.Event{
				Type:      "error",
				SessionID: request.SessionID,
				Error:     event.Err.Error(),
			})
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
					_ = stream.Send(httpx.Event{
						Type:      "error",
						SessionID: request.SessionID,
						Error:     err.Error(),
					})
					continue
				}
				if message.Content == "" {
					continue
				}
				assistant.WriteString(message.Content)
				if err := stream.Send(httpx.Event{
					Type:      "message",
					SessionID: request.SessionID,
					Agent:     event.AgentName,
					Content:   message.Content,
				}); err != nil {
					return
				}
			}
			continue
		}

		if output.Message != nil && output.Message.Content != "" {
			assistant.WriteString(output.Message.Content)
			if err := stream.Send(httpx.Event{
				Type:      "message",
				SessionID: request.SessionID,
				Agent:     event.AgentName,
				Content:   output.Message.Content,
			}); err != nil {
				return
			}
		}
	}

	if err := h.svc.Turns.Complete(ctx, turnID, identity.Subject, assistant.String()); err != nil {
		_ = stream.Send(httpx.Event{
			Type:      "error",
			SessionID: request.SessionID,
			Error:     "complete chat turn failed",
		})
		return
	}

	_ = stream.Send(httpx.Event{Type: "done", SessionID: request.SessionID})
}

// handleInterrupt persists the approval checkpoint and notifies the client.
func (h *streamHandlers) handleInterrupt(
	ctx context.Context,
	stream *httpx.Writer,
	event *adk.AgentEvent,
	sessionID, turnID, subject, runID, checkpointID string,
) {
	approvalID, ok := approvalIDFromInterrupt(event.Action.Interrupted.Data)
	if !ok {
		_ = stream.Send(httpx.Event{Type: "error", SessionID: sessionID, Error: "approval interrupt did not include an approval ID"})
		return
	}

	interruptID, ok := rootCauseInterruptID(event.Action.Interrupted.InterruptContexts)
	if !ok {
		_ = stream.Send(httpx.Event{Type: "error", SessionID: sessionID, Error: "approval interrupt did not include a root cause"})
		return
	}

	if err := h.svc.Approvals.AttachCheckpoint(ctx, approvalID, runID, checkpointID, interruptID); err != nil {
		_ = h.svc.Runs.MarkFailed(ctx, runID, "approval_checkpoint_attach_failed")
		_ = stream.Send(httpx.Event{Type: "error", SessionID: sessionID, Error: "save approval checkpoint failed"})
		return
	}

	if err := h.svc.Turns.MarkInterrupted(ctx, turnID, subject, approvalID, checkpointID); err != nil {
		_ = stream.Send(httpx.Event{Type: "error", SessionID: sessionID, Error: "mark chat turn interrupted failed"})
		return
	}

	if err := h.svc.Runs.MarkInterrupted(ctx, runID, approvalID); err != nil {
		_ = stream.Send(httpx.Event{Type: "error", SessionID: sessionID, Error: "mark interrupted run failed"})
		return
	}

	_ = stream.Send(httpx.Event{
		Type:       "approval_required",
		SessionID:  sessionID,
		ApprovalID: approvalID,
	})
}

// ResumeApproval resumes a run that was interrupted for approval, streaming the
// remaining answer as SSE.
func (h *streamHandlers) ResumeApproval(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	identity, ok := auth.IdentityFromContext(ctx)
	if !ok {
		httpx.Fail(ctx, w, httpx.Unauthorized("unauthorized"))
		return
	}

	// go-zero's router does not populate net/http's PathValue; path variables are
	// carried on the request context by pathvar.
	approvalID := pathvar.Vars(r)["id"]
	if approvalID == "" {
		httpx.Fail(ctx, w, httpx.BadRequest("approval id is required"))
		return
	}

	record, err := h.svc.Approvals.ClaimForResume(ctx, approvalID, identity.Subject)
	if err != nil {
		httpx.Fail(ctx, w, httpx.Conflict(err.Error()))
		return
	}

	runCtx := middleware.WithSession(ctx, record.SessionID)

	if record.CheckpointID == nil || record.InterruptID == nil {
		_ = h.svc.Approvals.ReleaseResume(runCtx, record.ID)
		httpx.Fail(ctx, w, httpx.Conflict("approval is missing its checkpoint"))
		return
	}

	iter, err := h.svc.Agent.Runner.ResumeWithParams(
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
		_ = h.svc.Approvals.ResetResuming(runCtx, approvalID)
		httpx.Fail(ctx, w, httpx.Internal("resume approval failed"))
		return
	}

	stream, err := httpx.NewWriter(w)
	if err != nil {
		httpx.Fail(ctx, w, httpx.Internal("streaming is unavailable"))
		return
	}
	defer func() { _ = stream.Close() }()

	h.streamResume(runCtx, stream, record, iter)
}

// streamResume forwards resume events to the client and finalizes the turn.
func (h *streamHandlers) streamResume(
	ctx context.Context,
	stream *httpx.Writer,
	record *approval.Request,
	iter *adk.AsyncIterator[*adk.AgentEvent],
) {
	var assistant strings.Builder

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}

		if event.Err != nil {
			_ = h.svc.Approvals.ReleaseResume(ctx, record.ID)
			_ = stream.Send(httpx.Event{
				Type:      "error",
				SessionID: record.SessionID,
				Error:     event.Err.Error(),
			})
			return
		}

		if event.Action != nil && event.Action.Interrupted != nil {
			_ = h.svc.Approvals.ReleaseResume(ctx, record.ID)
			_ = stream.Send(httpx.Event{
				Type:      "error",
				SessionID: record.SessionID,
				Error:     "resumed execution requires another approval",
			})
			return
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
					_ = h.svc.Approvals.ReleaseResume(ctx, record.ID)
					_ = stream.Send(httpx.Event{
						Type:      "error",
						SessionID: record.SessionID,
						Error:     err.Error(),
					})
					return
				}
				if message.Content == "" {
					continue
				}
				assistant.WriteString(message.Content)
				if err := stream.Send(httpx.Event{
					Type:      "message",
					SessionID: record.SessionID,
					Agent:     event.AgentName,
					Content:   message.Content,
				}); err != nil {
					return
				}
			}
			continue
		}

		if output.Message != nil && output.Message.Content != "" {
			assistant.WriteString(output.Message.Content)
			if err := stream.Send(httpx.Event{
				Type:      "message",
				SessionID: record.SessionID,
				Agent:     event.AgentName,
				Content:   output.Message.Content,
			}); err != nil {
				return
			}
		}
	}

	if record.TurnID == nil {
		_ = h.svc.Approvals.ReleaseResume(ctx, record.ID)
		_ = stream.Send(httpx.Event{
			Type:      "error",
			SessionID: record.SessionID,
			Error:     "approval is missing its chat turn",
		})
		return
	}

	if err := h.svc.Turns.Complete(ctx, *record.TurnID, record.RequestedBy, assistant.String()); err != nil {
		_ = h.svc.Approvals.ReleaseResume(ctx, record.ID)
		_ = stream.Send(httpx.Event{
			Type:      "error",
			SessionID: record.SessionID,
			Error:     "complete resumed chat turn failed",
		})
		return
	}

	_ = stream.Send(httpx.Event{Type: "done", SessionID: record.SessionID})
}

func decodeChatRequest(r *http.Request) (chatRequest, error) {
	var request chatRequest
	if r.Body == nil {
		return request, errors.New("request body is empty")
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return request, err
	}
	return request, nil
}

// approvalIDFromInterrupt extracts the approval id from interrupt data.
func approvalIDFromInterrupt(data any) (string, bool) {
	values, ok := data.(map[string]string)
	if !ok {
		return "", false
	}
	approvalID := values["approval_id"]
	return approvalID, approvalID != ""
}

// rootCauseInterruptID returns the id of the root-cause interrupt context.
func rootCauseInterruptID(contexts []*adk.InterruptCtx) (string, bool) {
	for _, item := range contexts {
		if item.IsRootCause && item.ID != "" {
			return item.ID, true
		}
	}
	return "", false
}

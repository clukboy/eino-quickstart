package server

import (
	"context"
	"eino-quickstart/internal/agent"
	"eino-quickstart/internal/approval"
	"eino-quickstart/internal/auth"
	"eino-quickstart/internal/middleware"
	"eino-quickstart/internal/observability"
	"eino-quickstart/internal/run"
	"eino-quickstart/internal/session"
	"eino-quickstart/internal/turn"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/goccy/go-json"
	"github.com/google/uuid"
)

type Server struct {
	Agent          *agent.Harness
	Sessions       *session.Store
	Approvals      *approval.Store
	Runs           *run.Store
	Turns          *turn.Store
	Authenticator  *auth.Authenticator
	Logger         *slog.Logger
	Metrics        *observability.Metrics
	MaxRequestBody int64
}

type ChatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

type Event struct {
	Type       string `json:"type"`
	SessionID  string `json:"session_id,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Content    string `json:"content,omitempty"`
	Error      string `json:"error,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
}

type ApprovalDecisionRequest struct {
	Approved bool `json:"approved"`
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	// 存活检查不认证，方便 Kubernetes / LB 探测。
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /ready", s.ready)

	mux.Handle("GET /metrics", auth.Require(auth.RoleAdmin)(s.Metrics.Handler()))

	mux.Handle(
		"POST /api/v1/sessions",
		auth.Require(auth.RoleAgent)(
			http.HandlerFunc(s.createSession),
		),
	)
	mux.Handle(
		"POST /api/v1/chat",
		auth.Require(auth.RoleAgent)(
			http.HandlerFunc(s.chat),
		),
	)
	mux.Handle(
		"GET /api/v1/approvals/{id}",
		auth.Require(auth.RoleApprover)(
			http.HandlerFunc(s.getApproval),
		),
	)
	mux.Handle(
		"POST /api/v1/approvals/{id}/decision",
		auth.Require(auth.RoleApprover)(
			http.HandlerFunc(s.decideApproval),
		),
	)

	mux.Handle(
		"POST /api/v1/approvals/{id}/resume",
		auth.Require(auth.RoleAgent)(
			http.HandlerFunc(s.resumeApproval),
		),
	)
	var handler http.Handler = mux
	handler = http.MaxBytesHandler(handler, s.MaxRequestBody)
	handler = s.Authenticator.Authenticate(handler)

	if s.Metrics != nil {
		handler = s.Metrics.Middleware(handler)
	}
	handler = observability.AccessLog(s.Logger, handler)
	handler = observability.Recover(s.Logger, handler)
	handler = observability.RequestID(handler)
	handler = observability.HTTPTrace(s.Logger, handler)
	return handler
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Sessions.Ready(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
		return
	}
	id := uuid.NewString()
	err := s.Sessions.GetOrCreate(r.Context(), id, identity.Subject)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session_id": id})
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "unauthorized",
		})
		return
	}
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is empty"})
		return
	}
	if req.SessionID == "" {
		req.SessionID = uuid.NewString()
	}

	history, err := s.Sessions.History(r.Context(), req.SessionID, identity.Subject)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	turnID := uuid.NewString()
	_, err = s.Turns.Start(
		r.Context(),
		turnID,
		req.SessionID,
		identity.Subject,
		req.Message,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "create chat turn failed",
		})
		return
	}
	history = append(history, agent.UserMessage(req.Message))
	history = s.Agent.Context.Prepare(history)

	setSSEHeaders(w)
	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}
	ctx := middleware.WithSession(r.Context(), req.SessionID)
	ctx = middleware.WithTurn(ctx, turnID)
	runID := uuid.NewString()
	if err := s.Runs.Create(
		r.Context(),
		runID,
		req.SessionID,
		identity.Subject,
		req.Message,
		time.Now().UTC().Add(24*time.Hour),
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "create agent run failed",
		})
		return
	}

	checkpointID := runID
	iter := s.Agent.Runner.Run(ctx, history, adk.WithCheckPointID(checkpointID), adk.WithSessionValues(map[string]any{"session_id": req.SessionID, "run_id": runID, "turn_id": turnID}))

	var assistant strings.Builder
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Action != nil &&
			event.Action.Interrupted != nil {
			approvalID, ok := approvalIDFromInterrupt(
				event.Action.Interrupted.Data,
			)
			if !ok {
				_ = sendEvent(w, flusher, Event{
					Type:      "error",
					SessionID: req.SessionID,
					Error:     "approval interrupt did not include an approval ID",
				})
				return
			}

			interruptID, ok := rootCauseInterruptID(
				event.Action.Interrupted.InterruptContexts,
			)
			if !ok {
				_ = sendEvent(w, flusher, Event{
					Type:      "error",
					SessionID: req.SessionID,
					Error:     "approval interrupt did not include a root cause",
				})
				return
			}

			if err := s.Approvals.AttachCheckpoint(
				r.Context(),
				approvalID,
				runID,
				checkpointID,
				interruptID,
			); err != nil {
				_ = s.Runs.MarkFailed(
					r.Context(),
					runID,
					"approval_checkpoint_attach_failed",
				)

				_ = sendEvent(w, flusher, Event{
					Type:      "error",
					SessionID: req.SessionID,
					Error:     "save approval checkpoint failed",
				})
				return
			}

			if err := s.Turns.MarkInterrupted(
				r.Context(),
				turnID,
				identity.Subject,
				approvalID,
				checkpointID,
			); err != nil {
				_ = sendEvent(w, flusher, Event{
					Type:      "error",
					SessionID: req.SessionID,
					Error:     "mark chat turn interrupted failed",
				})
				return
			}

			if err := s.Runs.MarkInterrupted(
				r.Context(),
				runID,
				approvalID,
			); err != nil {
				_ = sendEvent(w, flusher, Event{
					Type:      "error",
					SessionID: req.SessionID,
					Error:     "mark interrupted run failed",
				})
				return
			}

			_ = sendEvent(w, flusher, Event{
				Type:       "approval_required",
				SessionID:  req.SessionID,
				ApprovalID: approvalID,
			})

			return
		}

		if event.Err != nil {
			_ = sendEvent(w, flusher, Event{
				Type:      "error",
				SessionID: req.SessionID,
				Error:     event.Err.Error(),
			})
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		mo := event.Output.MessageOutput
		if mo.IsStreaming {
			for {
				msg, err := mo.MessageStream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					_ = sendEvent(w, flusher, Event{
						Type:      "error",
						SessionID: req.SessionID,
						Error:     err.Error(),
					})
				}
				if msg.Content != "" {
					assistant.WriteString(msg.Content)
					if err := sendEvent(w, flusher, Event{
						Type:      "message",
						SessionID: req.SessionID,
						Agent:     event.AgentName,
						Content:   msg.Content,
					}); err != nil {
						return
					}
				}
			}
		} else if mo.Message != nil && mo.Message.Content != "" {
			assistant.WriteString(mo.Message.Content)
			if err := sendEvent(w, flusher, Event{Type: "message", SessionID: req.SessionID, Agent: event.AgentName, Content: mo.Message.Content}); err != nil {
				return
			}
		}
	}

	if err := s.Turns.Complete(
		r.Context(),
		turnID,
		identity.Subject,
		assistant.String(),
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "complete chat turn failed",
		})
		return
	}

	_ = sendEvent(w, flusher, Event{Type: "done", SessionID: req.SessionID})
}

func (s *Server) getApproval(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	record, ok := s.Approvals.Get(r.Context(), id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "approval not found"})
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "unauthorized",
		})
		return
	}
	id := r.PathValue("id")
	var req ApprovalDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.Approvals.Decide(r.Context(), id, req.Approved, identity.Subject); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) resumeApproval(
	w http.ResponseWriter,
	r *http.Request,
) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "unauthorized",
		})
		return
	}

	approvalID := r.PathValue("id")

	record, err := s.Approvals.ClaimForResume(
		r.Context(),
		approvalID,
		identity.Subject,
	)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": err.Error(),
		})
		return
	}

	if err := s.Approvals.MarkResuming(
		r.Context(),
		approvalID,
	); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": err.Error(),
		})
		return
	}

	ctx := middleware.WithSession(
		r.Context(),
		record.SessionID,
	)

	iter, err := s.Agent.Runner.ResumeWithParams(
		ctx,
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
		_ = s.Approvals.ResetResuming(
			r.Context(),
			approvalID,
		)

		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "resume approval failed",
		})
		return
	}

	setSSEHeaders(w)
	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	s.streamResume(
		w,
		flusher,
		r.Context(),
		record,
		iter,
	)
}

func setSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}

func sendEvent(w http.ResponseWriter, f http.Flusher, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data); err != nil {
		return err
	}
	f.Flush()
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func approvalIDFromInterrupt(data any) (string, bool) {
	values, ok := data.(map[string]string)
	if !ok {
		return "", false
	}

	approvalID := values["approval_id"]
	return approvalID, approvalID != ""
}

func rootCauseInterruptID(
	contexts []*adk.InterruptCtx,
) (string, bool) {
	for _, item := range contexts {
		if item.IsRootCause && item.ID != "" {
			return item.ID, true
		}
	}

	return "", false
}

func (s *Server) streamResume(w http.ResponseWriter, flusher http.Flusher, ctx context.Context, record *approval.Request, iter *adk.AsyncIterator[*adk.AgentEvent]) {
	var assistant strings.Builder

	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			_ = s.Approvals.ReleaseResume(ctx, record.ID)

			_ = sendEvent(w, flusher, Event{
				Type:      "error",
				SessionID: record.SessionID,
				Error:     event.Err.Error(),
			})
			return
		}

		if event.Action != nil &&
			event.Action.Interrupted != nil {
			_ = s.Approvals.ReleaseResume(ctx, record.ID)

			_ = sendEvent(w, flusher, Event{
				Type:      "error",
				SessionID: record.SessionID,
				Error:     "resumed execution requires another approval",
			})
			return
		}

		if event.Output == nil ||
			event.Output.MessageOutput == nil {
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
					_ = s.Approvals.ReleaseResume(ctx, record.ID)

					_ = sendEvent(w, flusher, Event{
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

				if err := sendEvent(w, flusher, Event{
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

		if output.Message != nil &&
			output.Message.Content != "" {
			assistant.WriteString(output.Message.Content)

			if err := sendEvent(w, flusher, Event{
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
		_ = s.Approvals.ReleaseResume(ctx, record.ID)

		_ = sendEvent(w, flusher, Event{
			Type:      "error",
			SessionID: record.SessionID,
			Error:     "approval is missing its chat turn",
		})
		return
	}

	if err := s.Turns.Complete(
		ctx,
		*record.TurnID,
		record.RequestedBy,
		assistant.String(),
	); err != nil {
		_ = s.Approvals.ReleaseResume(ctx, record.ID)

		_ = sendEvent(w, flusher, Event{
			Type:      "error",
			SessionID: record.SessionID,
			Error:     "complete resumed chat turn failed",
		})
		return
	}
	_ = sendEvent(w, flusher, Event{
		Type:      "done",
		SessionID: record.SessionID,
	})
}

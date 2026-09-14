package httpx

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/goccy/go-json"
)

// Event is one Server-Sent Event emitted on the chat / approval-resume stream.
// It keeps the same JSON field names as the net/http and Hertz transports so
// clients can switch transports without changes.
type Event struct {
	Type       string `json:"type"`
	SessionID  string `json:"session_id,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Content    string `json:"content,omitempty"`
	Error      string `json:"error,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
}

// Writer emits SSE frames over net/http.
//
// This exists instead of goctl's generated SSE because the generated handler
// writes unnamed frames only:
//
//	fmt.Fprintf(w, "data: %s\n\n", payload)   // goctl api/gogen/sse_handler.tpl
//
// The client contract for /api/v1/chat and /api/v1/approvals/:id/resume uses
// named events consumed via addEventListener(...). The event name is carried by
// an `event:` line that goctl never emits, so those two routes are registered
// by hand in restapi.go with rest.WithSSE() and write frames here, byte for byte
// the same as the Hertz implementation:
//
//	event: message
//	data: {"type":"message","session_id":"...","content":"..."}
//
// (blank line terminates the frame)
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter sets the SSE headers, commits the status line and returns a writer
// that flushes after every frame. It must be created only after all pre-flight
// work has succeeded, so that failures still return a normal JSON error rather
// than a half-open stream.
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("httpx: response writer does not support streaming")
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")

	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return &Writer{w: w, flusher: flusher}, nil
}

// Send writes a single named event. The SSE event name mirrors Event.Type so
// clients can use addEventListener(type, ...).
func (s *Writer) Send(event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event.Type, payload); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// Close flushes any buffered frame. Nothing is written on close because the
// terminal `done` event is an explicit frame.
func (s *Writer) Close() error {
	s.flusher.Flush()
	return nil
}

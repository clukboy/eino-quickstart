package middleware

import (
	"net/http"

	"github.com/zeromicro/go-zero/core/trace"
)

// TraceIDHeader is the response header carrying the request's trace id.
const TraceIDHeader = "X-Trace-ID"

// TraceID echoes the request's go-zero trace id back as X-Trace-ID.
//
// go-zero's TraceHandler puts the trace id into the W3C traceparent header (and
// into the logs) but never emits the bare id, so a client that wants to quote it
// in a bug report has to parse traceparent — a string that also carries the span
// id and sampling flags. This adds the direct header.
//
// Register it *before* Authenticate and the role checks (see restapi.Engine):
// server.Use middlewares run outside the route handlers, so registering first
// makes TraceID the outermost of ours and the header is present on the 401/403
// that auth produces — which is exactly when a caller needs it most.
//
// The id comes from trace.TraceIDFromContext, which is only populated once
// ServiceConf.SetUp() has installed a TracerProvider (restapi.Run does that).
// Without one it is the empty string, and no header is written: an empty
// X-Trace-ID is worse than none, because it looks like a real answer.
func TraceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := trace.TraceIDFromContext(r.Context()); id != "" {
			w.Header().Set(TraceIDHeader, id)
		}
		next.ServeHTTP(w, r)
	})
}

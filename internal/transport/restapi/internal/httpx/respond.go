package httpx

import (
	"context"
	"errors"
	"net/http"

	"eino-quickstart/internal/platform/observability"

	resthttpx "github.com/zeromicro/go-zero/rest/httpx"
)

// Stable machine-readable error codes. Clients should branch on these instead
// of parsing the human-readable error string. Identical to the net/http and
// Hertz transports so clients can switch transports without changes.
const (
	CodeBadRequest         = "bad_request"
	CodeUnauthorized       = "unauthorized"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeConflict           = "conflict"
	CodeUnavailable        = "service_unavailable"
	CodeInternal           = "internal_error"
	CodeTooLarge           = "payload_too_large"
	CodeUnsupportedFormat  = "unsupported_format"
	CodeInvalidCredentials = "invalid_credentials"
)

// ErrorResponse is the single error envelope for every endpoint. The error
// field keeps the historical shape ({"error": "..."}) so existing clients keep
// working, while code/request_id are additive.
type ErrorResponse struct {
	Code      string `json:"code"`
	Error     string `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

// Error is an application error carrying the HTTP status and stable code the
// transport should emit.
//
// Body, when set, replaces the standard envelope. Endpoints with a bespoke
// failure contract need it: the readiness probe answers 503 with
// {"status":"not ready","error":"..."}, which the generic envelope cannot
// express. NoBody marks a response with no payload at all.
//
// Both exist because go-zero's generated handler only gives the logic two
// outcomes: return (resp, nil) — always 200 with the response marshalled — or
// return (nil, err) — routed through the error handler. A 204 or a 202 with an
// empty body therefore has to travel on the error. The two knowledge-base
// binding endpoints and the upload placeholder need exactly that.
type Error struct {
	Status  int
	Code    string
	Message string
	Body    any
	NoBody  bool
}

func (e *Error) Error() string { return e.Message }

// New builds an application error.
func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// WithBody attaches a custom response body and returns the error for chaining.
func (e *Error) WithBody(body any) *Error {
	e.Body = body
	return e
}

// Empty marks the response as having no payload: the status is written and
// nothing else. Use it for 202/204 style successes that the (resp, err)
// signature cannot express.
func (e *Error) Empty() *Error {
	e.NoBody = true
	return e
}

// Typed constructors for the codes above. Each returns a fresh *Error so the
// WithBody/Empty modifiers can be chained without aliasing.
func BadRequest(message string) *Error {
	return New(http.StatusBadRequest, CodeBadRequest, message)
}

func Unauthorized(message string) *Error {
	return New(http.StatusUnauthorized, CodeUnauthorized, message)
}

func Forbidden(message string) *Error {
	return New(http.StatusForbidden, CodeForbidden, message)
}

func NotFound(message string) *Error {
	return New(http.StatusNotFound, CodeNotFound, message)
}

func Conflict(message string) *Error {
	return New(http.StatusConflict, CodeConflict, message)
}

func Unavailable(message string) *Error {
	return New(http.StatusServiceUnavailable, CodeUnavailable, message)
}

func TooLarge(message string) *Error {
	return New(http.StatusRequestEntityTooLarge, CodeTooLarge, message)
}

func Internal(message string) *Error {
	return New(http.StatusInternalServerError, CodeInternal, message)
}

// InvalidCredentials is the 401 the auth middleware emits for a malformed or
// unknown bearer token, as opposed to a missing identity.
func InvalidCredentials(message string) *Error {
	return New(http.StatusUnauthorized, CodeInvalidCredentials, message)
}

// Register installs the process-wide error handler that renders every failure
// through the unified envelope.
//
// Fallback semantics matter here. go-zero's own default handler answers every
// non-gRPC error with 400, because the only errors a generated handler produces
// on its own come from httpx.Parse (bad bindings / failed `validate` tags).
// Keeping that fallback means binding failures stay a 400 without this package
// having to know how to recognise them; logic that wants another status returns
// a typed *Error.
func Register() {
	resthttpx.SetErrorHandlerCtx(func(ctx context.Context, err error) (int, any) {
		var appErr *Error
		if errors.As(err, &appErr) {
			if appErr.NoBody {
				return appErr.Status, nil
			}
			if appErr.Body != nil {
				return appErr.Status, appErr.Body
			}
			return appErr.Status, ErrorResponse{
				Code:      appErr.Code,
				Error:     appErr.Message,
				RequestID: observability.RequestIDFromContext(ctx),
			}
		}

		return http.StatusBadRequest, ErrorResponse{
			Code:      CodeBadRequest,
			Error:     err.Error(),
			RequestID: observability.RequestIDFromContext(ctx),
		}
	})
}

// Fail routes err through the registered error handler. Middleware uses it
// because it has no (resp, err) return path to piggyback on.
func Fail(ctx context.Context, w http.ResponseWriter, err *Error) {
	resthttpx.ErrorCtx(ctx, w, err)
}

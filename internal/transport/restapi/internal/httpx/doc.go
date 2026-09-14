// Package httpx holds the transport-agnostic primitives shared by the go-zero
// handlers and middleware: the unified error envelope and the SSE writer.
//
// It is a leaf package on purpose. The generated logic lives in
// internal/logic/{admin,agent,approver,health}, which sits below this package,
// so anything both the logic and the root restapi package need has to live here
// to avoid an import cycle.
package httpx

// Package httpx holds the transport-agnostic primitives shared by the go-zero
// handlers and middleware: the unified error envelope.
//
// It is a leaf package on purpose. The generated logic lives in
// internal/logic/{admin,agent,approver,dataset,health,stream}, which sits below
// this package, so anything both the logic and the root restapi package need
// has to live here to avoid an import cycle.
//
// SSE is deliberately not here. It used to be — a hand-written frame writer
// (sse.go) serving the two routes that were registered by hand. Both are gone:
// the routes come from docs/stream/stream.api with `sse: true`, so the frames
// are written by goctl's generated handler and the only thing clients need to
// know about their shape is documented in that .api file.
package httpx

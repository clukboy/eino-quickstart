package observability

import (
	"context"
	"strconv"
	"time"
)

// ContextWithRequestID returns a context carrying requestID so that
// RequestIDFromContext and LogWithTrace work for transports that do not use the
// net/http RequestID middleware (for example Hertz).
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil || requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

// InFlightInc increments the in-flight request gauge.
func (m *Metrics) InFlightInc() {
	if m == nil {
		return
	}
	m.inFlight.Inc()
}

// InFlightDec decrements the in-flight request gauge.
func (m *Metrics) InFlightDec() {
	if m == nil {
		return
	}
	m.inFlight.Dec()
}

// ObserveRequest records one completed request. route should be the matched
// route pattern (e.g. "/api/v1/chat") rather than a raw path, to keep label
// cardinality bounded.
func (m *Metrics) ObserveRequest(method, route string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	if route == "" {
		route = "unknown"
	}
	m.requestsTotal.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.requestTime.WithLabelValues(method, route).Observe(duration.Seconds())
}

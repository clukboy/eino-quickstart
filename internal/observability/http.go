package observability

import (
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func HTTPTrace(
	logger *slog.Logger,
	next http.Handler,
) http.Handler {
	tracer := otel.Tracer("eino-harness/http")

	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		ctx := otel.GetTextMapPropagator().Extract(
			r.Context(),
			propagation.HeaderCarrier(r.Header),
		)

		ctx, span := tracer.Start(
			ctx,
			r.Method+" "+r.URL.Path,
		)
		defer span.End()

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func Recover(
	logger *slog.Logger,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		defer func() {
			if recovered := recover(); recovered != nil {
				LogWithTrace(
					r.Context(),
					logger,
				).Error(
					"http request panicked",
					"request_id",
					RequestIDFromContext(r.Context()),
					"method",
					r.Method,
					"path",
					r.URL.Path,
				)

				http.Error(
					w,
					`{"error":"internal server error"}`,
					http.StatusInternalServerError,
				)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func AccessLog(
	logger *slog.Logger,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		startedAt := time.Now()

		recorder := &statusRecorder{
			ResponseWriter: w,
		}

		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}

		route := r.Pattern
		if route == "" {
			route = "unknown"
		}

		span := trace.SpanFromContext(r.Context())
		span.SetAttributes(
			attribute.String(
				"http.request.method",
				r.Method,
			),
			attribute.String(
				"http.route",
				route,
			),
			attribute.Int(
				"http.response.status_code",
				status,
			),
		)

		if status >= http.StatusInternalServerError {
			span.SetStatus(
				codes.Error,
				http.StatusText(status),
			)
		}

		LogWithTrace(
			r.Context(),
			logger,
		).Info(
			"http request completed",
			"request_id",
			RequestIDFromContext(r.Context()),
			"method",
			r.Method,
			"route",
			route,
			"status",
			status,
			"duration_ms",
			time.Since(startedAt).Milliseconds(),
		)
	})
}

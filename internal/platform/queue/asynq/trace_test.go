package asynq

import (
	"context"
	"testing"

	"eino-quickstart/internal/platform/observability"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceEnvelopeKeepsHTTPToWorkerTraceID(t *testing.T) {
	provider, err := observability.NewTracerProvider(context.Background(), observability.TraceConfig{
		ServiceName: "acceptance",
		Environment: "test",
		SampleRatio: 1,
	})
	if err != nil {
		t.Fatalf("create tracer provider: %v", err)
	}
	defer provider.Shutdown(context.Background())

	rootCtx, root := observability.StartSpan(context.Background(), "http.upload")
	defer root.End()
	if !root.SpanContext().IsValid() {
		t.Fatal("root span is invalid")
	}

	publishCtx, publish := startPublishSpan(rootCtx, "knowledge:index", "index")
	defer publish.End()
	encoded, err := encodeEnvelope(publishCtx, []byte(`{"document_id":"42"}`))
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	carrier, payload := decodeEnvelope(encoded)
	if string(payload) != `{"document_id":"42"}` {
		t.Fatalf("payload changed: %s", payload)
	}

	workerCtx := observability.ExtractTrace(context.Background(), carrier)
	workerCtx, process := startProcessSpan(workerCtx, "knowledge:index")
	defer process.End()
	if got, want := trace.SpanContextFromContext(workerCtx).TraceID(), root.SpanContext().TraceID(); got != want {
		t.Fatalf("worker trace id = %s, want root trace id %s", got, want)
	}
	if got := trace.SpanContextFromContext(workerCtx).TraceID(); got != publish.SpanContext().TraceID() {
		t.Fatalf("publish and worker trace ids differ: %s vs %s", got, publish.SpanContext().TraceID())
	}
}

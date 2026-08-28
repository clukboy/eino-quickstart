package observability

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	_ "go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials/insecure"
)

type TraceConfig struct {
	ServiceName string
	Environment string
	Endpoint    string
	Insecure    bool
	SampleRatio float64
}

func NewTracerProvider(
	ctx context.Context,
	config TraceConfig,
) (*sdktrace.TracerProvider, error) {
	options := make([]otlptracegrpc.Option, 0, 2)

	options = append(
		options,
		otlptracegrpc.WithEndpoint(config.Endpoint),
	)

	if config.Insecure {
		options = append(
			options,
			otlptracegrpc.WithTLSCredentials(
				insecure.NewCredentials(),
			),
		)
	}

	exporter, err := otlptracegrpc.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf(
			"create OTLP trace exporter: %w",
			err,
		)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(config.ServiceName),
			semconv.DeploymentEnvironment(
				config.Environment,
			),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create trace resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(
			sdktrace.ParentBased(
				sdktrace.TraceIDRatioBased(
					config.SampleRatio,
				),
			),
		),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return provider, nil
}

func TraceFields(ctx context.Context) []any {
	spanContext := otel.SpanFromContext(ctx).SpanContext()

	if !spanContext.IsValid() {
		return nil
	}

	return []any{
		"trace_id", spanContext.TraceID().String(),
		"span_id", spanContext.SpanID().String(),
	}
}

func LogWithTrace(
	ctx context.Context,
	logger *slog.Logger,
) *slog.Logger {
	fields := TraceFields(ctx)

	if len(fields) == 0 {
		return logger
	}

	return logger.With(fields...)
}

func SpanError(span trace.Span, err error) {
	if err == nil {
		return
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

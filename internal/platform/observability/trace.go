package observability

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials/insecure"
)

// TracerName 是本项目自己的 instrumentation scope。go-zero 的 span 用的是
// core/trace.TraceName，两个名字并存是有意的：排查时能一眼分出「这个 span 是
// 框架给的」还是「这段是业务自己埋的」。
const TracerName = "eino-quickstart"

type TraceConfig struct {
	ServiceName string
	Environment string
	Endpoint    string
	Insecure    bool
	SampleRatio float64
}

// NewTracerProvider 安装全局 TracerProvider 与传播器。
//
// 两处刻意的取舍：
//
//   - **Endpoint 为空也照样装 provider**，只是没有 exporter。go-zero 的
//     trace.StartAgent 就是这个语义，这里对齐它：provider 在不在决定了 trace id
//     有没有值（span 是否有效），而 exporter 只决定它导不导出去。少了 provider
//     的话 `otel.Tracer()` 是空实现，队列里注入的 traceparent 会变成空的，
//     跨进程的链路直接断在投递处 —— 这种「没配端点就没链路」的静默降级最难查。
//   - **SampleRatio <= 0 当作 1.0**。`TraceIDRatioBased(0)` 的含义是「一条都不
//     采」，配置漏填一个字段就变成全丢，代价比多采几条大得多。
func NewTracerProvider(ctx context.Context, config TraceConfig) (*sdktrace.TracerProvider, error) {
	options := make([]sdktrace.TracerProviderOption, 0, 3)

	if config.Endpoint != "" {
		exporterOptions := make([]otlptracegrpc.Option, 0, 2)
		exporterOptions = append(
			exporterOptions,
			otlptracegrpc.WithEndpoint(config.Endpoint),
		)

		if config.Insecure {
			exporterOptions = append(
				exporterOptions,
				otlptracegrpc.WithTLSCredentials(
					insecure.NewCredentials(),
				),
			)
		}

		exporter, err := otlptracegrpc.New(ctx, exporterOptions...)
		if err != nil {
			return nil, fmt.Errorf(
				"create OTLP trace exporter: %w",
				err,
			)
		}
		options = append(options, sdktrace.WithBatcher(exporter))
	}

	// 这里必须用 NewSchemaless 而不是 NewWithAttributes(semconv.SchemaURL, ...)：
	// resource.Default() 自带 SDK 那个 semconv 版本的 Schema URL，再 Merge 一个
	// 手写的 Schema URL，两边版本不一致就会 Fatal 成
	// 「conflicting Schema URL: .../1.43.0 and .../1.26.0」—— 而这是个错误返回，
	// 组合根照常启动失败。属性键仍然用 semconv/v1.26.0 的常量（服务名、环境名
	// 在两个版本里没有差别），只是不给这个 resource 声明 schema 版本。
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(config.ServiceName),
			semconv.DeploymentEnvironment(
				config.Environment,
			),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create trace resource: %w", err)
	}

	ratio := config.SampleRatio
	if ratio <= 0 {
		ratio = 1.0
	}

	options = append(
		options,
		sdktrace.WithResource(res),
		// ParentBased 是跨进程链路能连起来的另一半：有上游 span 时跟着上游的
		// 采样决定走，两个进程就不会一个采一个不采、给出半截链路。
		sdktrace.WithSampler(
			sdktrace.ParentBased(
				sdktrace.TraceIDRatioBased(ratio),
			),
		),
	)

	provider := sdktrace.NewTracerProvider(options...)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return provider, nil
}

// Tracer 返回项目统一的 tracer。不要在包里缓存它的返回值：provider 是进程启动
// 之后才装上的，缓存下来的可能是空实现。
func Tracer() trace.Tracer {
	return otel.Tracer(TracerName)
}

// StartSpan 开一个 span，并把带 span 的 ctx 交回给调用方。
//
// 与直接用 otel.Tracer().Start 的区别只有一处：scope 名统一，跨进程的两个进程
// 之间能在 trace 界面上落到同一个 instrumentation 分组里。
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, opts...)
}

// InjectTrace 把 ctx 里的链路上下文导出成可跨进程携带的键值对（W3C traceparent）。
func InjectTrace(ctx context.Context) propagation.MapCarrier {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier
}

// ExtractTrace 从跨进程携带的键值对里恢复链路上下文。空 carrier 原样返回，
// 不给下游凭空造一个只有一半的上下文。
func ExtractTrace(ctx context.Context, carrier propagation.MapCarrier) context.Context {
	if len(carrier) == 0 {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// SetupTracing 按配置安装全局 TracerProvider，返回的 shutdown 用于在进程退出前
// 冲刷还在缓冲里的 span。
//
// 只有 worker 进程需要它。restapi 进程**不要**调这个：go-zero 的
// ServiceConf.SetUp() 已经装了 provider（配置在 etc/restapi.yaml 的 Telemetry
// 段），而 otel.SetTracerProvider 是全局的、后装覆盖先装 —— 两个都装会让其中
// 一方的 resource 配置（服务名、环境）静默失效。
func SetupTracing(ctx context.Context, cfg TraceConfig) (func(context.Context) error, error) {
	provider, err := NewTracerProvider(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return provider.Shutdown, nil
}

// TraceIDFromContext 返回 ctx 里当前 span 的 trace id，没有则返回空串。
//
// 只有 trace id、不含 span id 的用途（写响应头、写业务日志的关联字段）用这个；
// 要落日志字段就用 LogWithTrace。
func TraceIDFromContext(ctx context.Context) string {
	spanContext := trace.SpanFromContext(ctx).SpanContext()
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.TraceID().String()
}

func TraceFields(ctx context.Context) []any {
	spanContext := trace.SpanFromContext(ctx).SpanContext()

	if !spanContext.IsValid() {
		return nil
	}

	return []any{
		"trace_id", spanContext.TraceID().String(),
		"span_id", spanContext.SpanID().String(),
	}
}

func LogWithTrace(ctx context.Context, logger *slog.Logger) *slog.Logger {
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

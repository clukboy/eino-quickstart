package asynq

import (
	"context"
	"encoding/json"
	"fmt"

	"eino-quickstart/internal/platform/observability"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// envelopeVersion 是信封格式的版本号。
//
// 解码要靠它把「带信封的新消息」和「升级前遗留在队列里的裸 payload」分开，
// 所以它必须参与判定：只看有没有 body 字段的话，一个恰好带 body 键的业务
// payload 会被误判成信封，然后被当成空任务处理。
const envelopeVersion = 1

// envelope 是真正投进 Redis 的字节。
//
// 为什么要加一层信封，而不是往业务 payload 里塞一个 traceparent 字段：跨进程
// 链路是**传输层**的关注点。payload 是业务契约（见 platform/queue/tasks），让它
// 长出链路字段等于每新增一种任务都要记得带上它，忘一次就断一条链路；放在
// 适配器里，所有任务类型自动获得透传，业务代码一行都不用改。
//
// 代价是 Redis 里的 payload 多了一层壳，且升级期间队列里会同时存在带信封与
// 不带信封的消息 —— 所以解码必须容错，见 decodeEnvelope。
type envelope struct {
	Version int                    `json:"v"`
	Trace   propagation.MapCarrier `json:"trace,omitempty"`
	Body    json.RawMessage        `json:"body"`
}

// encodeEnvelope 把业务 payload 包进信封，并把当前 span 上下文写进 trace。
//
// 调用点必须在 producer span 之内：注入的是「投递这个动作」的 span 上下文，
// 消费端因此会挂到它下面，两个进程落在同一条 trace 上。
func encodeEnvelope(ctx context.Context, payload []byte) ([]byte, error) {
	raw, err := json.Marshal(envelope{
		Version: envelopeVersion,
		Trace:   observability.InjectTrace(ctx),
		Body:    payload,
	})
	if err != nil {
		return nil, fmt.Errorf("asynq: encode task envelope: %w", err)
	}
	return raw, nil
}

// decodeEnvelope 拆信封，返回链路上下文与业务 payload。
//
// 三种输入都要能正确处理，因为它们在生产里都会出现：
//
//   - 正常信封：拆出 trace 与 body。
//   - 升级前的裸 payload：`v` 对不上或没有 body，原样当作 payload 返回。滚动
//     升级时队列里的存量消息就属于这一类，认不出来会让积压的任务全部变成
//     「解码失败」而反复重试。
//   - 连 JSON 都不是的 payload：原样返回。payload 本来就可以是任意字节，不是
//     所有任务都必须用 JSON 编码。
//
// 任何情况下都不返回错误：解码失败不该变成一个业务错误，它只是「这条消息没有
// 链路信息」而已。
func decodeEnvelope(payload []byte) (propagation.MapCarrier, []byte) {
	var env envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, payload
	}
	if env.Version != envelopeVersion || len(env.Body) == 0 {
		return nil, payload
	}
	return env.Trace, env.Body
}

// messagingAttributes 是队列 span 上共用的语义属性，便于在 trace 界面上按
// 队列名和任务类型过滤。
func messagingAttributes(taskType, queueName, operation string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("messaging.system", "asynq"),
		attribute.String("messaging.operation", operation),
		attribute.String("messaging.message.type", taskType),
	}
	if queueName != "" {
		attrs = append(
			attrs,
			attribute.String("messaging.destination.name", queueName),
		)
	}
	return attrs
}

// startPublishSpan 开一个投递 span，返回带 span 的 ctx。
//
// SpanKind=Producer 让 trace 界面上「投递」与「消费」是两个方向明确的操作，
// 而不是两个看不出关系的本地调用。
func startPublishSpan(ctx context.Context, taskType, queueName string) (context.Context, oteltrace.Span) {
	return observability.StartSpan(
		ctx,
		"queue.publish "+taskType,
		oteltrace.WithSpanKind(oteltrace.SpanKindProducer),
		oteltrace.WithAttributes(
			messagingAttributes(taskType, queueName, "publish")...,
		),
	)
}

// startProcessSpan 开一个消费 span。
//
// 这里刻意用「子 span」而不是「独立 trace + link」：目标是「一篇文档从 HTTP 到
// 向量库是一条可点的链路」，子 span 让两个进程共享同一个 trace id，用户拿到
// 日志里的 trace id 就能看到全貌。队列重投时 payload 里的 traceparent 不变，
// 于是每次重试都是同一个投递 span 下的一个新子 span，重试轨迹也能看全。
func startProcessSpan(ctx context.Context, taskType string) (context.Context, oteltrace.Span) {
	return observability.StartSpan(
		ctx,
		"queue.process "+taskType,
		oteltrace.WithSpanKind(oteltrace.SpanKindConsumer),
		oteltrace.WithAttributes(
			messagingAttributes(taskType, "", "process")...,
		),
	)
}

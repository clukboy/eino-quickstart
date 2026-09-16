package hook

import (
	"context"
	"eino-quickstart/ent"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TraceHook 同时记录 Trace Span 和操作审计信息
func TraceAuditHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			// ✅ 从 ctx 中提取 Tracer，保证与 go-zero 链路关联
			tracer := trace.SpanFromContext(ctx).TracerProvider().Tracer("entgo")

			// 动态生成 Span 名称，如 "ent.mutate.User.Create"
			spanName := fmt.Sprintf("ent.%s.%s", m.Op(), m.Type())
			ctx, span := tracer.Start(ctx, spanName)
			defer span.End()

			// 注入关键属性
			span.SetAttributes(
				attribute.String("ent.table", m.Type()),
				attribute.String("ent.operation", m.Op().String()),
			)

			// 记录操作人（假设你的中间件已将 userID 注入 ctx）
			if userID, ok := ctx.Value("user_id").(int64); ok {
				span.SetAttributes(attribute.Int64("operator.id", userID))
			}

			start := time.Now()

			// ⚠️ 必须调用 next，否则请求不会到达数据库
			v, err := next.Mutate(ctx, m)

			duration := time.Since(start)
			span.SetAttributes(attribute.Float64("duration_ms", float64(duration.Milliseconds())))

			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetStatus(codes.Ok, "")
			}

			return v, err
		})
	}
}

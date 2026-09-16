package queue

import "context"

// RetryState 是一次任务执行所处的重试位置。
//
// handler 需要知道自己是不是最后一次机会，才能决定「失败」要不要落成终态：
// 还有重试余量时应该把错误抛回去让队列再试一次，用完了才该把业务状态标成
// 失败。这个信息只有队列实现知道，所以由适配器注入 context，handler 从
// context 取 —— 应用层因此不必 import 具体的 MQ 客户端。
type RetryState struct {
	// Attempt 是本次是第几次执行，从 0 开始。
	Attempt int
	// Max 是重试次数上限：含首次共执行 Max+1 次。
	Max int
	// Known 为 false 表示当前上下文没有重试信息（例如直接调用 handler 的
	// 单测），此时 handler 应保守地不写终态。
	Known bool
}

// Exhausted 报告当前是否是最后一次机会。Known 为 false 时返回 false ——
// 拿不到重试信息就不该判定「已耗尽」，否则一次偶发错误会被写成终态失败。
func (r RetryState) Exhausted() bool {
	if !r.Known {
		return false
	}
	return r.Attempt >= r.Max
}

type retryStateKey struct{}

// WithRetryState 把重试位置挂到 context 上，由队列适配器在调用 handler 前完成。
func WithRetryState(ctx context.Context, state RetryState) context.Context {
	return context.WithValue(ctx, retryStateKey{}, state)
}

// RetryStateFrom 取出重试位置。没有注入过时返回零值（Known=false）。
func RetryStateFrom(ctx context.Context) RetryState {
	state, _ := ctx.Value(retryStateKey{}).(RetryState)
	return state
}

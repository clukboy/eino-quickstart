package asynq

import (
	"context"
	"errors"
	"strconv"
	"time"

	"eino-quickstart/internal/platform/queue"
	"eino-quickstart/internal/platform/queue/tasks"
)

// IndexQueueConfig 是索引任务投递侧的参数。队列名不从配置读：任务契约已经
// 定死了 index 队列（tasks.QueueIndex），允许这里覆盖只会让投递与消费两侧
// 的口径分叉。
type IndexQueueConfig struct {
	// MaxRetries 是单个任务的重试次数上限，含首次共执行 MaxRetries+1 次。
	MaxRetries int
	// Timeout 是单个任务的处理上限，0 表示用契约里的默认值。
	Timeout time.Duration
}

// IndexQueue 把「把一篇文档排进索引队列」这个动作收敛成一个方法。
//
// 它实现的是应用层定义的 IndexTaskQueue 端口，但这里刻意**不 import 应用层**：
// Go 的接口是隐式的，只要方法签名对得上就自动满足，依赖方向因此保持在
// 「应用层 → 端口 ← 适配器」，适配器不必知道调用者是谁。
type IndexQueue struct {
	producer   queue.Producer
	maxRetries int
	timeout    time.Duration
}

// NewIndexQueue 包装一个 Producer。producer 为 nil（例如队列被关闭）时不
// panic：投递失败会作为错误浮到请求上，比进程起不来更容易定位。
func NewIndexQueue(producer queue.Producer, cfg IndexQueueConfig) *IndexQueue {
	return &IndexQueue{
		producer:   producer,
		maxRetries: cfg.MaxRetries,
		timeout:    cfg.Timeout,
	}
}

// EnqueueIndex 投递一篇文档的索引任务。
//
// 返回值必须被调用方当作请求错误处理：投递失败意味着这篇文档的索引永远
// 不会发生，静默返回成功会把文档永久留在 indexing 状态而没人知道。
func (q *IndexQueue) EnqueueIndex(ctx context.Context, datasetID, documentID uint64) error {
	if q == nil || q.producer == nil {
		return errors.New("asynq: index queue is not configured")
	}

	options := make([]tasks.Option, 0, 3)
	options = append(options, tasks.WithQueue(tasks.QueueIndex))
	if q.maxRetries > 0 {
		options = append(options, tasks.WithMaxRetries(q.maxRetries))
	}
	if q.timeout > 0 {
		options = append(options, tasks.WithTimeout(q.timeout))
	}

	return tasks.EnqueueKnowledgeIndex(ctx, q.producer, tasks.KnowledgeIndexPayload{
		DatasetID:  strconv.FormatUint(datasetID, 10),
		DocumentID: strconv.FormatUint(documentID, 10),
	}, options...)
}

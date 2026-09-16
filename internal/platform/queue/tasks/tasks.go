// Package tasks 是 HTTP 进程与 worker 进程之间的任务契约。
//
// 拆分后 cmd/restapi 只投递、cmd/worker 只消费，两个进程之间没有函数调用，
// 唯一的连接点是共享的 Redis 队列和本包：两边 import 同一组任务类型常量与
// payload 结构。任务类型字符串和 payload 字段属于线上契约 —— 改字段形状时
// 两侧要按能容忍旧 payload 的顺序发布（handler 必须幂等，见下）。
package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"eino-quickstart/internal/platform/queue"
)

// 任务类型标识。命名用「域:动作」，队列里一眼能看出归属。
const (
	// TypeKnowledgeIndex 让一篇文档的索引追上它的内容。payload 只带身份标识
	// 不带工作清单：handler 收到后重新查询还缺哪些分块没索引，这样重试天然
	// 收敛（重放一个已完成的任务是无害的空转），文档在重试间隙被编辑也不会
	// 丢更新。
	TypeKnowledgeIndex = "knowledge:index"

	// QueueIndex 是索引任务专用队列名。索引任务耗时长、可延迟、失败要重试，
	// 和将来的短任务混在一个队列里会让长任务把并发槽位占满；分开之后 index
	// 队列的并发度可以单独调，权重也能单独配。
	QueueIndex = "index"
)

// KnowledgeIndexPayload 是 TypeKnowledgeIndex 的载荷。
//
// 用字符串而不是数值，是为了让 payload 在 Redis 里可读、也能容忍将来的
// 非数值标识；handler 侧解析失败会当错误抛出交给重试，不静默跳过。
type KnowledgeIndexPayload struct {
	DatasetID  string `json:"dataset_id"`
	DocumentID string `json:"document_id"`
}

// 默认的投递参数。调用方没有显式覆盖时用这些值。
const (
	// defaultTimeout 是单个任务的处理上限。索引一篇文档要跑完整条 embedding
	// 链路，比常规短任务宽松得多，但不设上限会让一个卡死的任务永久占住并发槽。
	defaultTimeout = 30 * time.Minute
)

// Option 在构造消息时覆盖默认投递参数，全部由适配器侧传入 —— 任务契约本身
// 不决定队列名和重试次数，那是部署配置。
type Option func(*queue.TaskMessage)

// WithQueue 指定任务投到哪个队列。
func WithQueue(name string) Option {
	return func(msg *queue.TaskMessage) {
		msg.Options[queue.OptionQueue] = name
	}
}

// WithMaxRetries 指定单个任务的重试次数上限（含首次共执行 n+1 次）。
func WithMaxRetries(n int) Option {
	return func(msg *queue.TaskMessage) {
		msg.Options[queue.OptionRetry] = n
	}
}

// WithTimeout 指定单个任务的处理超时。
func WithTimeout(d time.Duration) Option {
	return func(msg *queue.TaskMessage) {
		msg.Options[queue.OptionTimeout] = d
	}
}

// EncodeKnowledgeIndex 把载荷序列化成队列消息体。
func EncodeKnowledgeIndex(p KnowledgeIndexPayload) ([]byte, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode %s payload: %w", TypeKnowledgeIndex, err)
	}
	return raw, nil
}

// DecodeKnowledgeIndex 是 worker 侧的解码入口，带最小校验：结构坏掉或关键字段
// 为空的任务直接返回错误，交给 asynq 的重试/归档机制，而不是静默吞掉。
func DecodeKnowledgeIndex(payload []byte) (KnowledgeIndexPayload, error) {
	var p KnowledgeIndexPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return KnowledgeIndexPayload{}, fmt.Errorf("decode %s payload: %w", TypeKnowledgeIndex, err)
	}
	if p.DocumentID == "" {
		return KnowledgeIndexPayload{}, fmt.Errorf("decode %s payload: document_id is required", TypeKnowledgeIndex)
	}
	return p, nil
}

// KnowledgeIndexMessage 按契约构造一条索引任务消息。适配器负责决定队列名、
// 重试次数和超时，所以这些都以 Option 传入而不是写死在这里。
func KnowledgeIndexMessage(p KnowledgeIndexPayload, opts ...Option) (*queue.TaskMessage, error) {
	raw, err := EncodeKnowledgeIndex(p)
	if err != nil {
		return nil, err
	}
	msg := &queue.TaskMessage{
		Type:    TypeKnowledgeIndex,
		Payload: raw,
		Options: map[queue.Option]any{
			queue.OptionQueue:   QueueIndex,
			queue.OptionTimeout: defaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(msg)
	}
	return msg, nil
}

// EnqueueKnowledgeIndex 是投递入口。投递失败必须作为错误浮出来，不允许
// 「接口返回成功但任务永远没人处理」。
func EnqueueKnowledgeIndex(
	ctx context.Context,
	producer queue.Producer,
	p KnowledgeIndexPayload,
	opts ...Option,
) error {
	if producer == nil {
		return errors.New("queue producer is not available")
	}
	msg, err := KnowledgeIndexMessage(p, opts...)
	if err != nil {
		return err
	}
	return producer.Enqueue(ctx, msg)
}

package queue

import "context"

type Option string

const (
	OptionQueue     Option = "queue"
	OptionRetry     Option = "retry"
	OptionTimeout   Option = "timeout"
	OptionProcessAt Option = "process_at"
	OptionProcessIn Option = "process_in"
)

// TaskMessage 统一的任务消息结构，屏蔽底层 MQ 的消息格式差异
type TaskMessage struct {
	Type    string         // 任务类型标识，如 "order:cancel"
	Payload []byte         // 业务参数载荷
	Options map[Option]any // 扩展选项（延迟、优先级、重试次数等）
}

// Producer 生产者接口：负责任务投递
type Producer interface {
	Enqueue(ctx context.Context, msg *TaskMessage) error
	Close() error
}

// Consumer 消费者接口：负责任务订阅与处理
type Consumer interface {
	// Register 注册任务处理器
	Register(taskType string, handler TaskHandler)

	// Start 启动消费循环
	Start()

	Stop()
}

// TaskHandler 统一的任务处理函数签名（与上一轮推荐的标准签名一致）
type TaskHandler func(ctx context.Context, payload []byte) error

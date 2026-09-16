package asynq

import (
	"context"
	"eino-quickstart/internal/platform/queue"
	"fmt"
	"sync"
	"time"

	"github.com/hibiken/asynq"
	"github.com/zeromicro/go-zero/core/logx"
)

type AsynqClient struct {
	*asynq.Client
	*asynq.Server
	handler map[string]asynq.HandlerFunc

	mu       sync.RWMutex
	done     chan struct{}
	stopOnce sync.Once
}

// AsynqConf is the configuration struct for Asynq.
type AsynqConf struct {
	Addr        string
	Username    string
	Pass        string
	DB          int
	Concurrency int
	Enable      bool

	// Queues 是队列名到权重的映射。必须显式列出：asynq 默认只消费 "default"
	// 队列，而索引任务投的是 "index"，漏配会出现「投递成功但永远没人消费」。
	Queues map[string]int

	// MaxRetries 是投递侧的默认重试上限（含首次共执行 MaxRetries+1 次）。
	MaxRetries int

	RetryDelaySeconds    int
	MaxRetryDelaySeconds int

	// ShutdownTimeoutSeconds 是优雅关闭上限。索引任务动辄跑几分钟，默认的
	// 20s 会让进程一退出就把任务丢回队列重来，所以这个值要往宽了配。
	ShutdownTimeoutSeconds int
}

// DefaultQueueName 是没显式配 queues 时消费的队列。
const DefaultQueueName = "index"

const (
	defaultRetryDelay    = 30 * time.Second
	defaultMaxRetryDelay = time.Hour
	defaultShutdown      = 20 * time.Second
)

// QueuesOrDefault 回落到只消费 index 队列。
func (c *AsynqConf) QueuesOrDefault() map[string]int {
	if len(c.Queues) > 0 {
		return c.Queues
	}
	return map[string]int{DefaultQueueName: 1}
}

// NewRedisOpt returns a redis options from Asynq Configuration.
func (c *AsynqConf) NewRedisOpt() *asynq.RedisClientOpt {
	return &asynq.RedisClientOpt{
		Network:  "tcp",
		Addr:     c.Addr,
		Username: c.Username,
		Password: c.Pass,
		DB:       c.DB,
	}
}

func NewAsynqClient(c *AsynqConf) *AsynqClient {
	return &AsynqClient{
		Client:  c.newClient(),
		Server:  c.newServer(),
		handler: make(map[string]asynq.HandlerFunc),
		done:    make(chan struct{}),
	}
}

// NewClient returns a client from the configuration.
func (c *AsynqConf) newClient() *asynq.Client {
	if c.Enable {
		return asynq.NewClient(c.NewRedisOpt())
	} else {
		return nil
	}
}

// newServer returns a worker from the configuration.
func (c *AsynqConf) newServer() *asynq.Server {
	if !c.Enable {
		return nil
	}
	return asynq.NewServer(
		c.NewRedisOpt(),
		asynq.Config{
			Queues:      c.QueuesOrDefault(),
			Concurrency: c.Concurrency,

			// IsFailure 判定「不再重试」的任务。这里不改默认语义（err != nil
			// 即失败），只把最终失败记进日志 —— 用户可见的终态由 handler 在
			// 重试耗尽时自己写，队列这条日志是兜底的观测面。
			IsFailure: func(err error) bool {
				logx.Errorf("asynq task failed permanently, err: %+v", err)
				return true
			},
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				logx.WithContext(ctx).Errorf("handle task err, task:%s, err:%s", string(task.Payload()), err)
			}),
			RetryDelayFunc:  c.retryDelayFunc(),
			ShutdownTimeout: c.shutdownTimeout(),
			Logger: &logger{
				Logger: logx.WithContext(context.TODO()),
			},
		},
	)
}

// retryDelayFunc 指数退避：embedding 服务抖动或限流时，密集重试只会加重
// 症状。倍数封顶到 2^6，再被 MaxRetryDelaySeconds 截断。
func (c *AsynqConf) retryDelayFunc() asynq.RetryDelayFunc {
	base := time.Duration(c.RetryDelaySeconds) * time.Second
	if base <= 0 {
		base = defaultRetryDelay
	}
	ceiling := time.Duration(c.MaxRetryDelaySeconds) * time.Second
	if ceiling <= 0 {
		ceiling = defaultMaxRetryDelay
	}
	if ceiling < base {
		ceiling = base
	}
	return func(n int, _ error, _ *asynq.Task) time.Duration {
		if n < 0 {
			n = 0
		}
		if n > 6 {
			n = 6
		}
		delay := base << uint(n)
		if delay <= 0 || delay > ceiling {
			return ceiling
		}
		return delay
	}
}

func (c *AsynqConf) shutdownTimeout() time.Duration {
	if c.ShutdownTimeoutSeconds > 0 {
		return time.Duration(c.ShutdownTimeoutSeconds) * time.Second
	}
	return defaultShutdown
}

var _ queue.Producer = (*AsynqClient)(nil)

var _ queue.Consumer = (*AsynqClient)(nil)

// Enqueue implements [queue.Producer].
func (a *AsynqClient) Enqueue(ctx context.Context, msg *queue.TaskMessage) error {
	if a.Client == nil {
		return fmt.Errorf("asynq client is not enabled")
	}
	opts := make([]asynq.Option, 0, len(msg.Options))
	for key, opt := range msg.Options {
		switch key {
		case queue.OptionQueue:
			opts = append(opts, asynq.Queue(opt.(string)))
		case queue.OptionRetry:
			opts = append(opts, asynq.MaxRetry(opt.(int)))
		case queue.OptionTimeout:
			opts = append(opts, asynq.Timeout(opt.(time.Duration)))
		case queue.OptionProcessAt:
			opts = append(opts, asynq.ProcessAt(opt.(time.Time)))
		case queue.OptionProcessIn:
			opts = append(opts, asynq.ProcessIn(opt.(time.Duration)))
		}
	}
	task := asynq.NewTask(msg.Type, msg.Payload)
	_, err := a.Client.EnqueueContext(ctx, task, opts...)
	return err
}

// Register implements [queue.Consumer].
//
// handler 拿到的 ctx 里带着本次执行的重试位置（queue.RetryStateFrom）：那种
// 「失败要不要落终态」的判断只有队列知道答案，应用层因此不必 import asynq。
func (a *AsynqClient) Register(taskType string, handler queue.TaskHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.handler[taskType]; exists {
		panic(fmt.Sprintf("handler for task type %s already exists", taskType))
	}
	a.handler[taskType] = func(ctx context.Context, task *asynq.Task) error {
		attempt, attemptOK := asynq.GetRetryCount(ctx)
		maxRetry, maxOK := asynq.GetMaxRetry(ctx)
		ctx = queue.WithRetryState(ctx, queue.RetryState{
			Attempt: attempt,
			Max:     maxRetry,
			Known:   attemptOK && maxOK,
		})
		return handler(ctx, task.Payload())
	}
}

// Start 启动消费循环。注册必须发生在调用 Start 之前。
//
// 两个关键点：
//   - 不用 Server.Run：它内部自己监听 SIGTERM/SIGINT 并调用 Shutdown，和
//     go-zero proc 的信号关闭链是两套处理，混用会让优雅关闭时序不可控。
//   - asynq v0.26 起 Server.Start 拉起消费 goroutine 后立即返回，这里必须
//     自己阻塞到 Stop 被调用，否则 ServiceGroup 会认为 worker 已经跑完，
//     进程随之开始关闭。
func (a *AsynqClient) Start() {
	if a.Server == nil {
		logx.Error("asynq server is disabled, nothing to start")
		return
	}
	mux := asynq.NewServeMux()
	for taskType, handler := range a.handler {
		mux.HandleFunc(taskType, handler)
	}
	if err := a.Server.Start(mux); err != nil {
		panic(fmt.Sprintf("failed to start asynq server: %v", err))
	}
	<-a.done
}

// Stop 幂等：ServiceGroup.Stop 和 proc 的关闭链可能都会调到它。
// Shutdown 优雅排空：已在跑的任务最多再活 asynq.Config.ShutdownTimeout，
// 超时的任务退回队列由下次启动的实例重新领取。
func (a *AsynqClient) Stop() {
	if a.Server == nil {
		return
	}
	a.stopOnce.Do(func() {
		a.Server.Shutdown()
		close(a.done)
	})
}

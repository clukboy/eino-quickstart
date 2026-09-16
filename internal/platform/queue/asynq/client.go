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
	mu      sync.RWMutex
}

// AsynqConf is the configuration struct for Asynq.
type AsynqConf struct {
	Addr         string
	Username     string
	Pass         string
	DB           int
	Concurrency  int
	SyncInterval int
	Enable       bool
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

// NewServer returns a worker from the configuration.
func (c *AsynqConf) newServer() *asynq.Server {
	if c.Enable {
		return asynq.NewServer(
			c.NewRedisOpt(),
			asynq.Config{
				IsFailure: func(err error) bool {
					fmt.Printf("failed to exec asynq task, err : %+v \n", err)
					return true
				},
				Concurrency: c.Concurrency,
				ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
					logx.WithContext(ctx).Errorf("handle task err, task:%s, err:%s", string(task.Payload()), err)
				}),
				Logger: &logger{
					Logger: logx.WithContext(context.TODO()),
				},
			},
		)
	} else {
		return nil
	}
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
func (a *AsynqClient) Register(taskType string, handler queue.TaskHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.handler[taskType]; exists {
		panic(fmt.Sprintf("handler for task type %s already exists", taskType))
	}
	a.handler[taskType] = func(ctx context.Context, task *asynq.Task) error {
		return handler(ctx, task.Payload())
	}
}

// Start 启动消费循环
func (a *AsynqClient) Start() {
	mux := asynq.NewServeMux()
	for taskType, handler := range a.handler {
		mux.HandleFunc(taskType, handler)
	}
	err := a.Run(mux)
	if err != nil {
		panic(fmt.Sprintf("failed to start asynq server: %v", err))
	}
}

// Stop implements [queue.Consumer].
func (a *AsynqClient) Stop() {
	a.Server.Shutdown()
}

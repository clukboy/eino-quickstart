// Package knowledge 是知识库用例层。
//
// 它承担「一份文件从落盘到可检索」这件事的全部编排：校验、正文落盘、文档行
// 与分块行的生命周期、把索引任务排进队列，以及 worker 侧消费任务后的 embedding
// 与向量写入。传输层只表达领域意图（「重建这篇文档的索引」），队列的队列名、
// payload 编码、重试次数都收在这里和适配器里，transport 不 import asynq。
//
// 依赖方向：
//
//	transport(restapi) → application/knowledge → platform/queue(Producer 抽象)
//	                                          → platform/queue/asynq(适配器) → Redis
//	cmd/worker → application/knowledge(Indexer) ← 同一个 Redis
package knowledge

import (
	"errors"
	"fmt"

	"eino-quickstart/internal/rag"
)

// 领域错误。传输层按哨兵值映射成 HTTP 状态码，所以这些错误不能带内部细节
// （SQL 文本、文件路径），需要给客户端看的话要走 ValidationError。
var (
	ErrDatasetNotFound  = errors.New("knowledge: dataset not found")
	ErrDocumentNotFound = errors.New("knowledge: document not found")
	ErrSourceConflict   = errors.New("knowledge: document source already exists")

	// ErrContentUnavailable 表示 documents.source 指向的正文读不回来：文件被
	// 移走、删掉或超出上限。这是调用方能修的状态，不是服务端故障。
	ErrContentUnavailable = errors.New("knowledge: document content is unavailable")

	// ErrContentNotManaged 表示试图改写一个不属于托管目录的正文文件。注册进来
	// 的外部文件是调用方的资产，接口不该无声覆盖。
	ErrContentNotManaged = errors.New("knowledge: document content is not managed by the api")

	// ErrQueueUnavailable 表示索引任务投不出去。必须浮到请求上：静默成功会
	// 让文档永久停在 indexing。
	ErrQueueUnavailable = errors.New("knowledge: index queue is not available")
)

// 正文相关的哨兵错误直接复用 rag 的定义，避免同一种失败在两处各有一套判断。
var (
	ErrContentTooLarge    = rag.ErrContentTooLarge
	ErrContentOutsideRoot = rag.ErrContentOutsideRoot
)

// ValidationError 是「调用方参数不对」的错误，Message 是写给客户端看的，
// 因此构造时必须避免写入内部细节。
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// invalid 构造一个对客户端可见的校验错误。
func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

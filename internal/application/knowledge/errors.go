// Package knowledge 是知识库用例层。
//
// 它承担「一份文件从收下到可检索」这件事的全部编排：校验、正文落库（
// documents.content）、文档行与分块行的生命周期、把索引任务排进队列，以及
// worker 侧消费任务后的 embedding 与向量写入。传输层只表达领域意图（「重建这篇
// 文档的索引」），队列的队列名、payload 编码、重试次数都收在这里和适配器里，
// transport 不 import asynq。
//
// 正文只有一处真相：documents.content。它不对应任何磁盘文件 —— 写进来的是它，
// 索引时读的是它，召回时补全文读的也是它。所以这个包里没有任何路径拼接，也
// 没有任何「文件不见了」的失败形态。
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
)

// 领域错误。传输层按哨兵值映射成 HTTP 状态码，所以这些错误不能带内部细节
// （SQL 文本、字段值），需要给客户端看的话要走 ValidationError。
var (
	ErrDatasetNotFound  = errors.New("knowledge: dataset not found")
	ErrDocumentNotFound = errors.New("knowledge: document not found")
	ErrSourceConflict   = errors.New("knowledge: document source already exists")

	// ErrContentTooLarge 表示正文超过 knowledge.maxDocumentBytes。
	//
	// 本地定义而不是复用 rag 的哨兵：那条错误原本属于「文件太大」，而正文已经
	// 不在文件里了。两个概念共用一条错误，会让传输层的映射看起来像在讲文件。
	ErrContentTooLarge = errors.New("knowledge: document content is too large")

	// ErrQueueUnavailable 表示索引任务投不出去。必须浮到请求上：静默成功会
	// 让文档永久停在 indexing。
	ErrQueueUnavailable = errors.New("knowledge: index queue is not available")
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

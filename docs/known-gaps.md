# 待完善项

本文只记录已确认会影响生产部署或功能边界的事项。RAG 文件扫描、分块、队列驱动的 Embedding 与 Milvus 索引 worker 已实现；其本地验证流程见 [RAG 本地验证](rag-testing.md)。

## P0：检索质量评测与发布门禁

**状态：基础数据集已具备，尚未接入发布流程**

当前 `internal/eval/datasets/` 已有检索用例和阈值，但不会在部署前自动执行，也没有持续跟踪 Recall@K、无答案准确率、引用正确性、ACL 泄漏数和 P95 延迟。

完成标准：

1. 建立覆盖产品型号、同义表达、跨文档、无答案和私有文档越权场景的版本化金标集。
2. 在 CI 或发布流水线执行 `cmd/rag-test` 的非交互评测模式，并以 `thresholds.yaml` 阻断不达标发布。
3. 按 embedding 模型、切块策略和检索配置记录对比结果，避免参数改动造成隐性质量回归。

## P1：Rerank 接入

**状态：未实现**

`retrieval.enableRerank` 和 `Reranker` 接口已经预留，但 `HybridRetriever` 尚未接收或调用 reranker；当前配置为 `true` 不会改变最终排序。

完成标准：

1. 接入一个可配置的 reranker，在 ACL 过滤后对有限候选集重排。
2. 明确超时、降级和错误策略，并记录 rerank 前后排名以便诊断。
3. 增加重排生效、超时降级和私有文档不泄漏的测试。

## P1：知识库文件上传与批量导入

**状态：接口已实现，但不接收 multipart**

`/api/v1/dataset/:id/documents` 下的文档增删改查与重建索引已经落地：正文可以直接
放在 JSON 请求体里（`content`），也可以只给 `source` 注册一个已经在 `knowledge.root`
里的文件。索引是异步的，见本文末尾的「文档索引的异步边界」。

仍未实现的是 `multipart/form-data` 上传与目录批量导入：当前没有流式的文件接收
路径，超过 `runtime.maxRequestBodyBytes` 的文档无法通过接口写入，只能先落到
`knowledge.root` 再按 `source` 注册。

完成标准：

1. 提供流式 multipart 接收，把文件直接落进托管目录，避免整个正文进内存。
2. 提供目录级批量导入，按 `knowledge.root` 扫描并幂等登记。
3. 对私有文档保存上传者为 `owner_subject`，并覆盖 ACL 与审计测试。

## P1：Skill 未授予默认 Agent

**状态：设计待决**

启动过程会注册 `list_skills` 和 `load_skill`，但默认专项 Agent 未取得它们。因此 `skills/` 中的内容不会被默认 Agent 实际加载。

完成标准：

1. 明确哪些专项 Agent 可以使用 Skill。
2. 将所需 Skill 工具加入对应的 `registry.Require(...)`。
3. 对受策略保护的 Agent，同步更新 `security.allowedTools`。
4. 添加 Agent 路由测试。

## P2：检索可观测性与全量重建

**状态：文档生命周期已闭环，指标与版本切换缺席**

按文档查询状态、重建索引、删除文档并清理对应向量已经实现（`POST /api/v1/dataset/:id/documents/:docId/reindex`、
`POST /api/v1/dataset/:id/reindex`、`DELETE .../documents/:docId`）。Indexer 会记录
批处理数量与失败原因，但**没有指标**：队列积压、重试/失败数、各检索通道命中数和
检索延迟都只能翻日志。

另一个缺口是 embedding 模型、维度或 Milvus 集合变更时的**整库重建与版本切换**：
现在重建会原地覆盖同一个集合，切换期间新旧向量混在一起，没有灰度和回滚路径。

完成标准：

1. 暴露摄取量、队列积压、重试/失败数、向量/关键词/型号通道命中数和检索延迟指标。
2. 为 embedding 模型、维度或集合变更设计全量重建和版本切换流程。

## 文档索引的异步边界

文档写入路径（创建、更新正文、重建索引、上传）在请求内只做三件事：落正文、
建文档行、往 asynq 的 `index` 队列投一个任务。**切块不在请求内做** —— 一份
文件里有几个产品、每个产品多长，要等 worker 走完 `rag.Pipeline` 的拆分与切块
之后才知道，请求内算不出 `chunk_count`。worker 消费该任务，依次完成拆产品块、
切块、embedding、向量写入，并把 `document.status` 收敛成 `ready`。

由此带来五条运维前提，部署前需要确认：

1. **索引链路多了一个必需依赖：Redis。** 投递失败会让写文档的请求直接报错
   （`asynq.enabled=false` 时同样如此），而不是「先落库、后台慢慢重试」。这是刻意
   的取舍：队列不可用时的静默降级，最终会变成文档卡在 `indexing` 而没人知道。
   Redis 的连通性在进程启动时就探测一次，地址或密码配错当场失败。
2. **worker 是独立进程，必须单独部署。** 它是 `cmd/worker`，消费 `asynq.queues`
   里列出的队列（默认只有 `index`）。`cmd/restapi` 只投递，不消费。
   - `asynq.enabled=false` 时 worker 拒绝启动（没有可消费的队列，空转没有意义）；
     而 HTTP 侧仍然会起来，写文档会在投递那一步报错。
   - 漏配 `asynq.queues` 会让任务投进 `index` 却永远没人领 —— asynq 默认只消费
     `default` 队列，这是个安静的坑，所以配置校验要求 `queues` 非空。
   - worker 的依赖比 HTTP 重：PostgreSQL、Milvus、embedding 服务缺一个都起不来
     （启动即校验，而不是等任务进来才失败）。
3. **任务以文档为粒度，重试按 chunk 收敛。** 一个坏段落不会让整篇文档白跑：worker
   每次只挑还 `pending` 的 chunk，重试自动跳过上一轮已成功的部分。次数耗尽
   （`asynq.maxRetries`）后，剩下的 chunk 会被标成 `failed`，`document.status` 随之
   变 `failed`，需要人工触发一次 reindex。已经归档的任务留在 asynq 的归档队列里，
   超过保留期会被清理。
   - **幂等靠 `document.metadata.content_hash`。** 只有正文真的变了才重切分块；
     哈希一致时直接跳到补 `pending` 的分块，上一轮已成功的部分不会被推倒重来。
   - **重试耗尽才落终态**，判断依据是从 context 取的 `queue.RetryState`（由 asynq
     适配器注入 `GetRetryCount/GetMaxRetry`）。`ctx` 被取消（进程在退出）时不写
     终态 —— 那会把一次优雅关闭变成用户看到的「索引失败」。
4. **删除路径上的向量清理是同步且尽力而为的。** Milvus 不参与关系库事务，
   清理失败只记警告：孤儿向量取不回来（检索要回到 chunk 行做过滤），但会一直
   占着向量库空间，需要靠后续的全量重建收拾。HTTP 进程连不上 Milvus 时也能启动，
   退化成「只摘索引不删向量」。
5. **`chunk_count` / `indexed_chunk_count` 是实时聚合出来的，不是列。** ent 的
   `Document` 上没有这两个字段，接口每次从 `document_chunks` 分组统计。切块在
   worker 内完成，请求返回时给不出准确值，维护冗余列只会让它慢慢漂。

## P1：产品拆分目前只认 front-matter（待替换为 LLM 拆分）

**状态：接缝已就位，实现是默认桩**

摄取链路的形状是「一份文件 → N 个产品块 → 每块再切 chunk」，这两步都在
`rag.Pipeline` 的 ingest 链里完成（`cmd/worker` 与 `cmd/ragserver` 共用同一份
实现）。拆产品的接缝就是 eino 原生的 parser 接口，通过 `rag.Config.Parser` 注入：

```go
type Parser interface {
    Parse(ctx context.Context, reader io.Reader, opts ...parser.Option) ([]*schema.Document, error)
}
```

默认实现是 `rag/parser.ProductParser`，它按正文里的 `---` front-matter 分节 ——
因为眼下上传的文件本来就是 LLM 生成好的、带分节结构的 Markdown，这个默认桩能
直接跑通全链路。目标形态是换成 LLM 拆分：把一份混合了多个产品的资料丢给模型，
由模型判定「这是几个产品、每个产品的边界在哪」。

**替换点只有一处**：组合根里 `rag.Config.Parser`（`cmd/worker/main.go`）。
`cmd/ragserver` 的 `FileLoader` 走同一个接缝，所以换一次两边同时生效；
`knowledge.Indexer` 与传输层都不用动。

替换时要守住三条约束，否则下游会以很难查的方式坏掉：

1. **输出契约不变。** 仍然返回 `[]*schema.Document`，`MetaData` 要继续携带
   `product_id` / `model` / `specs_from_doc` / `variants` 这组键（`_source` 由
   loader 通过 `parser.WithURI` 写入，`Indexer` 会把它统一成 `documents.source`），
   否则 chunk 元数据与检索侧的型号过滤条件会对不上。
2. **必须可重放。** worker 的重试是「按 chunk 收敛」的，同一个任务可能被投第二次。
   拆分结果要由内容决定而不是由调用次数决定（同一份正文两次 Parse 必须得到同样的
   块数与边界），否则 `content_hash` 的幂等会被绕过，chunk 行每轮都重新洗牌。
3. **失败要显式报错。** 拆不出来（模型超时、返回不可解析）就返回 error，让任务走
   重试并最终落到 `document.status=failed`。不要「拆失败就退化成整篇当一个块」——
   那会静默产出一个检索质量很差但看起来成功的索引。

另外，`Parse` 在 worker 进程内调用，所以新增的模型调用会占用
`asynq.shutdownTimeoutSeconds` 的排空预算；`rag.Pipeline` 的 `Config.Timeout`
（当前走默认 60s）也要一并放大，替换后这两个值都要重新评估。

## P1：生产数据库迁移策略

**状态：开发模式实现**

`internal/platform/storage/entx/database.go` 会在启动时调用 `client.Schema.Create`。这适合学习和本地开发；生产环境应采用受版本控制的 Ent/Atlas migration，避免多实例启动时改表。

## P2：运行状态与可观测性闭环

**状态：需补充验证**

运行、审批、对话轮次和 tracing 已有基础实现，但仍缺少覆盖中断、恢复、异常和超时路径的端到端测试；OTLP provider 也尚未在服务组合根中创建与关闭。

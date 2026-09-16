# RAG 本地验证

本文验证完整的文档索引链路：创建数据集、写入一篇 Markdown、请求内落库并投递任务、
由独立的 worker 进程切块并异步生成向量写进 Milvus。全程走 REST 接口，不需要往目录里
手工塞文件。

## 前置条件

1. PostgreSQL、Milvus、Redis 都可访问，且 `configs/config.yaml` 里的 `storage`、
   `milvus`、`embedding`、`asynq` 配置与实际环境一致。
2. 设置运行所需的环境变量（两个进程读同一份配置，所以都要设）：

   ```bash
   export EINO_STORAGE_PASSWORD=...
   export EINO_EMBEDDING_API_KEY=...   # worker 需要；没设 worker 会拒绝启动
   export EINO_API_KEY_ADMIN=...
   # Redis 需要口令时，把 asynq.redis.passwordEnv 设成下面这个名字
   export EINO_REDIS_PASSWORD=...
   ```

3. 在仓库根目录执行命令。正文落在 `knowledge.root`（`configs/config.yaml` 里当前是
   `./tests/knowledge`）下的 `documents/` 托管子目录里。

## 启动服务

需要**两个**进程，分别开一个终端：

```bash
# 终端 1：消费端，负责切块 + embedding + 写 Milvus
go run ./cmd/worker

# 终端 2：HTTP，负责接收请求 + 投递任务
go run ./cmd/restapi
```

- `cmd/worker` 启动时会先建/校验 Milvus collection（维度、metric、load 一次往返），
  然后才开 Consume。启动日志里出现 `eino worker started` 且 `queues` 字段含
  `index` 才说明它真的在领任务。
- `cmd/restapi` 监听 `internal/transport/restapi/etc/restapi.yaml` 里的 `Host`/`Port`
  （默认 8090）。
- 两个进程都会在启动时探一次 Redis，地址或密码不对会直接失败退出。
- 只起了 HTTP 没起 worker 时，写文档的请求**仍然成功**（正文落盘、任务进队列），
  文档会一直停在 `indexing` —— 这是最容易误判成 bug 的一种状态。

## 写入一篇文档

```bash
ADMIN=$EINO_API_KEY_ADMIN
BASE=http://127.0.0.1:8090/api/v1

# 建数据集，记下返回体里的 id
curl -sS -X POST "$BASE/dataset" \
  -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' \
  -d '{"name":"demo","description":"本地验证","visibility":"system","type":"document"}'

# 写入正文：content 会被写进 knowledge.root/documents/<datasetId>/ 下的托管文件
curl -sS -X POST "$BASE/dataset/1/documents" \
  -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' \
  -d '{"title":"guide","content":"# 标题\n\n正文……"}'
```

写入请求内的链路**不包含切块**：

```text
POST /dataset/:id/documents          ← cmd/restapi
  -> ContentStore 把正文写到 knowledge.root/documents/<id>/<slug>.md
  -> 事务写入 documents，status=indexing
  -> 提交后投递 asynq 任务（队列 index，类型 knowledge:index）
  -> 返回

consume                              ← cmd/worker（另一个进程）
  -> rag.Pipeline 的 ingest 链（与 cmd/ragserver 共用同一份实现）
     -> FileLoader 按 documents.source 把文件读回来
     -> Parser 按 front-matter 拆出 N 个产品块（当前实现，见 known-gaps）
     -> MarkdownChunker 逐块切分块（标题优先，超长按字符数滑窗）
  -> 事务写入 document_chunks，vector_status=pending
  -> 分批 embedding -> Milvus upsert -> chunk 标 indexed
  -> 全部分块追平后 documents.status = ready
```

请求**不等待 embedding，也不切块**：返回时 `chunk_count` 和 `indexed_chunk_count`
都还是 0 —— 因为「有几个分块」要等 worker 按内容拆出产品块才知道。这两个数是接口
每次从 `document_chunks` 实时聚合的，不是 `documents` 上的列，所以它们会随着 worker
推进而增长，不是「创建时就定好的值」。

## 等待索引完成

```bash
curl -sS "$BASE/dataset/1/documents/1" -H "Authorization: Bearer $ADMIN"
```

`status` 变成 `ready` 且 `indexed_chunk_count == chunk_count` 即完成。一直停在
`indexing` 说明 worker 没跑或 Redis 不可达；变成 `failed` 说明重试次数已用尽。

## 查库核对

```sql
SELECT
  d.source, d.title, d.status,
  c.chunk_index, c.heading_path, c.vector_status, c.indexed_at
FROM documents AS d
JOIN document_chunks AS c ON c.document_id = d.id
WHERE d.id = 1
ORDER BY c.chunk_index;
```

预期 `documents.status = 'ready'`，每行 `vector_status` 都是 `indexed`。

## 排错

- **写文档的请求直接报错**：队列不可用。投递失败会让写请求失败，不会静默落库
  —— 理由见 [待完善项](known-gaps.md) 里的「文档索引的异步边界」。
- **文档停在 `indexing` 且 `chunk_count` 恒为 0**：worker 没在跑，或者任务投进了
  没人消费的队列。
  1. 确认 `cmd/worker` 进程活着，日志里有 `eino worker started`；
  2. 比对 `asynq.queues`（HTTP 与 worker 读同一份 `configs/config.yaml`）——
     漏配会让任务投进 `index` 却永远没人领，asynq 默认只消费 `default`；
  3. 直接看队列积压：`redis-cli -n 0 llen 'asynq:{index}:pending'`。
- **文档变 `failed`**：任务重试耗尽（`asynq.maxRetries`）。日志里搜
  `index document failed`，`retries_exhausted=true` 的那条会带原始错误（常见是
  embedding 鉴权失败或 Milvus 不可达）；修好外部依赖后用
  `POST /dataset/1/documents/1/reindex` 重来。注意进程在优雅关闭时被取消的任务
  **不会**写终态，它会退回队列由下个实例重跑。
- **看队列里还剩什么**：`redis-cli -n 0 keys 'asynq:{index}:*'`。重试耗尽的任务
  在 `asynq:{index}:archived`，超过保留期会被 asynq 自己清掉。

## 单元测试

正文文件存储的边界（路径穿越、软链逃逸、只读已注册文件、大小上限）无需外部服务
即可验证：

```bash
go test ./internal/rag
```

## 检索

> **当前状态：`search_knowledge` 还没接上检索。** `cmd/restapi` 里注册的是
> bindings 版本的工具，它只会回答「当前主体被授权了哪些知识库」，不会真的去
> Milvus / PostgreSQL 召回内容 —— 检索适配（`retrieval.HybridRetriever`）还在
> RAG 改造的进行中部分。所以下面这条命令现在验证的是「Agent 路由 + 授权白名单」，
> 不是「召回质量」。

索引完成后，用 `agent` 角色的 Key 调对话接口：

```bash
curl -N http://127.0.0.1:8090/api/v1/chat \
  -H "Authorization: Bearer $EINO_API_KEY_DEVELOPER" \
  -H 'Content-Type: application/json' \
  -d '{"message":"概括 guide 的主要内容"}'
```

根 Agent 会把知识类问题路由给知识 Agent，后者调用 `search_knowledge`。检索接上之后，
返回的会带引用；产品型号（如 `H11`）会额外进入精确型号通道。要验证索引本身是否成功，
看上面的「等待索引完成」和「查库核对」两节 —— 那两节不依赖检索链路。

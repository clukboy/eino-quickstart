# RAG 本地验证

本文验证完整的文档索引链路：创建数据集、写入一篇 Markdown、切块入库、异步生成
向量并写入 Milvus。全程走 REST 接口，不需要往目录里手工塞文件。

## 前置条件

1. PostgreSQL、Milvus、Redis 都可访问，且 `configs/config.yaml` 里的 `storage`、
   `milvus`、`embedding`、`asynq` 配置与实际环境一致。
2. 设置运行所需的环境变量：

   ```bash
   export EINO_STORAGE_PASSWORD=...
   export EINO_EMBEDDING_API_KEY=...
   export EINO_REDIS_PASSWORD=...   # Redis 无密码时置空，并把 asynq.redis.passwordEnv 也置空
   export EINO_API_KEY_ADMIN=...
   ```

3. 在仓库根目录执行命令。正文写在 `knowledge.root`（默认 `./knowledge`）下的
   `documents/` 托管子目录里。

## 启动服务

```bash
go run ./cmd/restapi
```

HTTP 监听 `etc/restapi.yaml` 里的 `Host`/`Port`（默认 8090）。索引 worker 是同一个
进程里的 asynq 消费者，由 `indexer.enabled` 与 `asynq.enabled` 共同控制；启动日志
里出现 `queue: worker started` 才说明它真的在跑。

进程启动时会先探一次 Redis，地址或密码不对会直接失败退出。

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

写入请求内的链路：

```text
POST /dataset/:id/documents
  -> ContentStore 把正文写到 knowledge.root/documents/<id>/<slug>.md
  -> MarkdownChunker 切块（Markdown 标题优先，超长块按字符数滑窗）
  -> 事务写入 documents + document_chunks，状态置 indexing
  -> 提交后投递 asynq 任务（队列 index，类型 knowledge:index_document）
```

请求**不等待 embedding**：返回时 `chunk_count` 已是最终值，
`indexed_chunk_count` 还是 0。

之后由 worker 接手：分批调用 embedding -> Milvus upsert -> chunk 标 `indexed`
-> 文档状态收敛为 `ready`。

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
- **文档停在 `indexing`**：确认 `indexer.enabled` 与 `asynq.enabled`，再在日志里
  找有没有 `queue: worker started`。
- **文档变 `failed`**：任务重试耗尽。日志里搜 `index task exhausted retries`，
  修好外部依赖后用 `POST /dataset/1/documents/1/reindex` 重来。
- **看队列里还剩什么**：`redis-cli -n <db> keys 'asynq:{index}:*'`。重试耗尽的任务
  在 `asynq:{index}:archived`，超过保留期会被 asynq 自己清掉。

## 单元测试

正文文件存储的边界（路径穿越、软链逃逸、只读已注册文件、大小上限）无需外部服务
即可验证：

```bash
go test ./internal/rag
```

## 检索

索引完成后，用 `agent` 角色的 Key 调对话接口：

```bash
curl -N http://127.0.0.1:8090/api/v1/chat \
  -H "Authorization: Bearer $EINO_API_KEY_DEVELOPER" \
  -H 'Content-Type: application/json' \
  -d '{"message":"概括 guide 的主要内容"}'
```

根 Agent 会把知识类问题路由给知识 Agent，后者调用 `search_knowledge` 返回带引用
的结果。产品型号（如 `H11`）会额外进入精确型号通道。

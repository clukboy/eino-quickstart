# RAG 本地验证

本文验证完整的离线 RAG 写入链路：将一个 Markdown 文件放入知识库目录、按标题和字符上限分块、写入 PostgreSQL、生成向量并写入 Milvus。此流程不经过 HTTP 上传接口；当前“上传”即复制文件到受控的 `knowledge.root` 目录。

## 前置条件

1. PostgreSQL 和 Milvus 可访问，且 `configs/config.yaml` 中的 `storage`、`milvus`、`embedding` 配置与实际环境一致。
2. 已设置 `EINO_STORAGE_PASSWORD` 和 `EINO_EMBEDDING_API_KEY`。运行对话服务时还需要 README 中列出的模型与 API Key 环境变量。
3. 在仓库根目录执行命令。默认知识目录是 `./knowledge`，可通过修改 `knowledge.root` 指向其他目录。

## 上传并创建索引

```bash
mkdir -p knowledge
cp /absolute/path/to/your-document.md knowledge/guide.md
go run ./cmd/knowledge-worker
```

Worker 会按以下顺序执行：

```text
knowledge/guide.md
  -> Loader（仅 .md/.markdown/.txt/.text，UTF-8、大小和符号链接边界检查）
  -> Chunker（Markdown 标题边界优先，超长块按字符数与 overlap 滑窗切分）
  -> PostgreSQL documents + document_chunks + vector_outbox
  -> Embedding API
  -> Milvus collection
```

`cmd/knowledge-worker` 会一直运行，以处理新的或重试中的 outbox 记录。日志出现 `knowledge indexer processed: claimed=N completed=N retried=0 failed=0` 表示该批向量已完成；使用 `Ctrl-C` 停止 worker。

重复运行同一 source 会更新文档、删除旧分块，并通过 outbox 删除旧向量后写入新向量。因此请为每个文件保持稳定的相对路径。

## 检查分块与索引状态

连接 PostgreSQL 后执行：

```sql
SELECT
  d.source,
  d.title,
  d.status,
  c.chunk_index,
  c.heading_path,
  c.start_line,
  c.end_line,
  c.character_count,
  c.vector_status
FROM documents AS d
JOIN document_chunks AS c ON c.document_id = d.id
WHERE d.source = 'guide.md'
ORDER BY c.chunk_index;
```

全部完成后，`documents.status` 应为 `ready`，每个 `vector_status` 应为 `indexed`。若状态为 `indexing`，检查 worker 日志以及未完成的 outbox：

```sql
SELECT id, chunk_id, operation, status, attempts, last_error
FROM vector_outbox
WHERE status <> 'done'
ORDER BY id;
```

## 验证分块规则

默认配置为每块 1200 个 Unicode 字符、重叠 200 个字符。每个 Markdown 标题会开始一个新块；分块记录保留标题路径和原文行号，供检索引用使用。无需连接外部服务即可运行以下测试验证这部分规则：

```bash
go test ./internal/knowledge -run TestChunker
```

## 检索

索引成功后启动服务，然后以 `agent` 角色的 API Key 调用对话接口，在 `message` 中询问文档包含的内容。根 Agent 会将知识相关问题路由给 `knowledge_agent`，后者调用 `search_knowledge` 并返回带引用的结果。

也可以用 `go run ./cmd/rag-test` 检查完整问题的各通道召回、RRF 排序和最终引用。产品型号（如 `H11` 或 `H105G`）会额外进入精确型号通道。

```bash
curl -N http://127.0.0.1:8080/api/v1/chat \
  -H "Authorization: Bearer $EINO_API_KEY_DEVELOPER" \
  -H "Content-Type: application/json" \
  -d '{"message":"概括 guide.md 的主要内容"}'
```

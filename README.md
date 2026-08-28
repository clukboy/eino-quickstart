# Eino Harness

基于 [Eino](https://github.com/cloudwego/eino) ADK 的多 Agent 软件工程 Harness。服务将请求分派给知识库、工作区和自动化专项 Agent；RAG 使用 PostgreSQL 保存文档与分块、Milvus 保存向量，并通过 durable outbox 可靠地完成向量索引。

## 当前能力

| 能力 | 入口 | 说明 |
| --- | --- | --- |
| 对话服务 | `go run ./cmd/server` | 提供 API Key 认证的 SSE 对话 API。 |
| RAG 摄取与索引 | `go run ./cmd/knowledge-worker` | 扫描 `knowledge.root` 下的 Markdown/text 文件，切块、入库并持续消费向量 outbox。 |
| 维护任务 | `go run ./cmd/maintenance` | 清理过期审批、检查点和对话轮次。 |

## 文档

- [架构与运行说明](docs/architecture.md)：组件、数据流、配置和 HTTP API。
- [RAG 本地验证](docs/rag-testing.md)：上传 Markdown、检查分块、创建索引并检索。
- [扩展 Agent、工具与 Skill](docs/agent-development.md)：专项 Agent、工具和 Skill 的接入方式。
- [待完善项](docs/known-gaps.md)：尚未完成或需要生产化的能力。

## 快速开始

1. 准备 PostgreSQL 和 Milvus，并按部署环境修改 `configs/config.yaml`。配置内的地址仅是示例，不应直接用于生产。
2. 设置运行所需的环境变量：

   ```bash
   export EINO_MODEL_API_KEY=...
   export EINO_EMBEDDING_API_KEY=...
   export EINO_STORAGE_PASSWORD=...
   export EINO_API_KEY_DEVELOPER=...
   export EINO_API_KEY_APPROVER=...
   export EINO_API_KEY_ADMIN=...
   ```

3. 创建知识库目录并将 Markdown 放入其中。目录结构会成为文档的稳定 `source`：

   ```bash
   mkdir -p knowledge
   cp /path/to/your-document.md knowledge/
   ```

4. 启动知识库 worker。它会先扫描、切块和写入 PostgreSQL，然后创建 Milvus collection 并持续执行向量索引：

   ```bash
   go run ./cmd/knowledge-worker
   ```

5. 另开终端启动服务：

   ```bash
   go run ./cmd/server
   ```

服务默认监听 `:8080`；`GET /health` 用于存活检查，`GET /ready` 检查 PostgreSQL 连通性。完整的 RAG 验证命令见 [RAG 本地验证](docs/rag-testing.md)。

## 目录

| 路径 | 职责 |
| --- | --- |
| `cmd/server` | HTTP 服务组合根 |
| `cmd/knowledge-worker` | 知识文件摄取与向量索引 worker |
| `internal/application` | Agent 组装、上下文与工具中间件 |
| `internal/knowledge` | Loader、Chunker、摄取、索引、检索与向量存储 |
| `internal/platform` | 配置、认证、执行、可观测性与持久化 |
| `internal/transport/httpapi` | HTTP API 与 SSE 传输层 |
| `configs/config.yaml` | 本地开发配置 |

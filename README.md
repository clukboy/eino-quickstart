# Eino Harness

基于 [Eino](https://github.com/cloudwego/eino) ADK 的多 Agent 软件工程 Harness。服务将请求分派给知识库、工作区和自动化专项 Agent；RAG 使用 PostgreSQL 保存文档与分块、Milvus 保存向量，文档索引由 asynq（Redis）队列驱动，写请求不等待 embedding。

## 当前能力

| 能力 | 入口 | 说明 |
| --- | --- | --- |
| 对话 + 知识库 API | `go run ./cmd/restapi` | 一个进程同时提供 API Key 认证的 SSE 对话 API 和知识库 admin 接口。**只投递不消费**：收到文档写请求时把正文落盘、建文档行、往 asynq（Redis）投一个索引任务就返回。 |
| 索引 worker | `go run ./cmd/worker` | 独立的消费进程：领任务后读回正文、拆产品块、切块、调 embedding、写 Milvus，并把文档状态收敛到 `ready` / `failed`。 |
| RAG 调试入口 | `go run ./cmd/ragserver` | 本地手动跑一遍 RAG 链路的调试用入口，不是生产运行形态。 |

两个进程之间没有函数调用，唯一的连接是共享 Redis 队列 + `internal/platform/queue/tasks`
里的任务契约，所以它们可以分别部署、分别重启。拆分带来的一个必要依赖：**Redis 不可用
时文档写请求会直接失败**（这是刻意的，避免文档静默卡在 `indexing`）。

维护清理（过期审批、检查点、对话轮次）的实现在 `internal/maintenance`，目前**没有**
入口进程把它接起来。

## 文档

- [架构与运行说明](docs/architecture.md)：组件、数据流、配置和 HTTP API。
- [RAG 本地验证](docs/rag-testing.md)：上传 Markdown、检查分块、创建索引并检索。
- [扩展 Agent、工具与 Skill](docs/agent-development.md)：专项 Agent、工具和 Skill 的接入方式。
- [待完善项](docs/known-gaps.md)：尚未完成或需要生产化的能力。
- [数据集接口契约快照](docs/dataset-pending-contract.md)：前端 `agent-platform` 对齐用的字段清单，正文锚定在 git `e4569e6`，路径表述早于 restapi 改造，以 `internal/transport/restapi/docs/*.api` 为准。

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

4. 启动 worker。它会在启动时就校验 PostgreSQL / Milvus / embedding 三者可用（缺一个就
   拒绝启动，而不是等任务进来才失败），然后开始消费队列：

   ```bash
   go run ./cmd/worker
   ```

5. 另开终端启动 HTTP 服务：

   ```bash
   go run ./cmd/restapi
   ```

服务默认监听 `:8080`；`GET /health` 用于存活检查，`GET /ready` 检查 PostgreSQL 连通性。
`asynq.enabled=false` 时 worker 拒绝启动，HTTP 仍可起来但文档写操作会报错。完整的 RAG
验证命令见 [RAG 本地验证](docs/rag-testing.md)。

## 目录

| 路径 | 职责 |
| --- | --- |
| `cmd/restapi` | HTTP 组合根：对话 API + 知识库 API，只投递索引任务 |
| `cmd/worker` | 索引消费组合根：读正文、拆产品块、切块、embedding、写向量 |
| `cmd/ragserver` | RAG 链路的手动调试入口 |
| `internal/transport/restapi` | HTTP API、SSE 传输层与 goctl 契约（`docs/*.api`） |
| `internal/application` | Agent 组装、上下文与工具中间件、`knowledge` 用例 |
| `internal/rag` | Loader、parser、Chunker、embedder 与向量存储（Milvus） |
| `internal/platform` | 配置、认证、执行、可观测性、队列与持久化 |
| `configs/config.yaml` | 本地开发配置（含 `asynq` 队列段） |

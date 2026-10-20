# Eino Harness

基于 [Eino](https://github.com/cloudwego/eino) ADK 的知识库问答服务，面向**服务器部署**：一个 Agent 同时承担知识库检索与通用对话，只有 `search_knowledge` 一个工具，没有本地工作区文件读写、shell 执行或审批流。RAG 使用 PostgreSQL 保存文档与分块、Milvus 保存向量、Elasticsearch 保存关键词索引（BM25），文档索引由 asynq（Redis）队列驱动，写请求不等待 embedding。

## 当前能力

| 能力 | 入口 | 说明 |
| --- | --- | --- |
| 对话 + 知识库 API | `go run ./cmd/restapi` | 一个进程同时提供 API Key 认证的 SSE 对话 API 和知识库 admin 接口。**只投递不消费**：收到文档写请求时把正文写进 `documents.content`、建文档行、往 asynq（Redis）投一个索引任务就返回。 |
| 索引 worker | `go run ./cmd/worker` | 独立的消费进程：领任务后从库里读正文（空则从旧的托管目录导入一次）、切块、调 embedding、写 Milvus、写关键词索引（配了 `ES.Address` 时），并把文档状态收敛到 `ready` / `failed`。 |
| RAG 调试入口 | `go run ./cmd/ragserver` | 本地手动跑一遍 RAG 链路的调试用入口，不是生产运行形态。 |
| 召回评测 | `go run ./cmd/rag-test` | 拿金标用例集打真实检索链路，输出 Recall@K / MRR / ACL 泄漏 / P95，并按 `internal/eval/thresholds.yaml` 判定退出码 —— 可以直接卡在 CI 或发布流水线里阻断质量回退。`internal/eval/thresholds.yaml` 是对应的阈值文件。 |

两个进程之间没有函数调用，唯一的连接是共享 Redis 队列 + `internal/platform/queue/tasks`
里的任务契约，所以它们可以分别部署、分别重启。拆分带来的一个必要依赖：**Redis 不可用
时文档写请求会直接失败**（这是刻意的，避免文档静默卡在 `indexing`）。

检索链路（精确 + 关键字 + 向量三个通道，加权 RRF 融合）已经实现，`cmd/rag-test` 跑的就是它；
关键字通道配了 `ES.Address` 时走 Elasticsearch BM25（产品型号、系列、品类等元数据字段
是主要命中面），没配则回落 PostgreSQL 子串匹配；精确通道走 ES 的 `term`，对型号这类
逐字查询按独立权重优先，未配 ES 时该通道降级并在响应里标注。**检索面由一份映射声明驱动**
（`ES.MappingFile`，默认 `configs/es/chunk_mapping.yaml`）：索引里有哪些业务字段、各自
权重、值从元数据的哪个路径取，都在这一处声明 —— 换产品线改文件即可，不用改代码。
HTTP 侧的 `POST /api/v1/dataset/:id/search` 与对话侧的 `search_knowledge` 共用同一条
混合检索装配链路；对话工具会在每次调用时解析认证主体的知识库授权，并输出带 source、章节
和 chunk id 的引用。检索质量持续评测与发布门禁由 `internal/eval` 负责。

维护清理（过期对话轮次）的实现在 `internal/maintenance`，目前**没有**入口进程把它接起来。

## 文档

接口契约以 `internal/transport/restapi/docs/*.api` 为准（入口 `restapi.api`），
生成物是 `internal/transport/restapi/internal/{handler,logic,types}` 与 `restapi.json`。
改接口只动对应的 `.api` 子文件，再执行 `make gen-api`。

## 快速开始

1. 准备 PostgreSQL、Redis、Milvus，以及可选但强烈建议的 Elasticsearch，并按部署环境分别修改
   `configs/api/config.yaml` 和 `configs/worker/config.yaml`（对应数据源必须一致）。
   HTTP 参数单独配置在 `configs/api/restapi.yaml`；隔离验收请用 `configs/acceptance/`。如果本机使用 Docker Desktop，可先执行
   `EINO_STORAGE_PASSWORD=... bash scripts/acceptance/dependencies.sh up` 启动本地验收依赖。
   配置内的地址仅是示例，不应直接用于生产。ES 留空时系统照常
   运行，只是关键词通道搜不到产品型号（型号只存在于分块元数据里）。
2. 设置运行所需的环境变量：

   ```bash
   export EINO_MODEL_API_KEY=...
   export EINO_EMBEDDING_API_KEY=...
   export EINO_STORAGE_PASSWORD=...
   export EINO_ES_PASSWORD=...      # 配了 ES.PasswordEnv 时需要
   export EINO_API_KEY_DEVELOPER=...
   export EINO_API_KEY_ADMIN=...
   export EINO_ANONYMOUS_SECRET=...
   export EINO_ACCOUNT_SECRET=...
   ```

3. 先检查配置（只校验，不连接服务）。worker 不需要上面的 API 身份或对话模型凭据：

   ```bash
   go run ./cmd/worker -check-config
   go run ./cmd/restapi -check-config
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

**配置覆盖：** API 业务用 `EINO_API_CONFIG`，API HTTP 用 `EINO_REST_CONFIG`，worker 用 `EINO_WORKER_CONFIG`；不再读取共享的 `EINO_CONFIG`。

服务默认监听 `:8090`（以 API HTTP 配置的 `Port` 为准）；`GET /health` 用于存活检查，`GET /ready` 检查 PostgreSQL 连通性。
`Asynq.Enabled=false` 时 worker 拒绝启动，HTTP 仍可起来但文档写操作会报错。完整的 RAG
验证命令见 `cmd/rag-test`。

## 目录

| 路径 | 职责 |
| --- | --- |
| `cmd/restapi` | HTTP 组合根：对话 API + 知识库 API，只投递索引任务 |
| `cmd/worker` | 索引消费组合根：读正文、切块、embedding、写向量与关键词索引 |
| `cmd/ragserver` | RAG 链路的手动调试入口 |
| `internal/transport/restapi` | HTTP API、SSE 传输层与 goctl 契约（`docs/*.api`） |
| `internal/application` | Agent 组装、上下文与工具中间件、`knowledge` 用例 |
| `internal/rag` | Loader、parser、Chunker、embedder 与向量存储（Milvus） |
| `internal/platform` | 配置、认证、可观测性、队列、持久化与关键词索引（`storage/es`） |
| `configs/api/config.yaml` / `restapi.yaml` | API 业务配置 / HTTP 监听配置 |
| `configs/worker/config.yaml` | 独立索引消费配置 |
| `configs/acceptance` | API / worker 分离的隔离验收模板 |
| `configs/config.yaml` | 历史完整配置，新入口不再默认读取 |

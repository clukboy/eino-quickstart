# 架构与运行说明

## 分层

项目按职责分为四层，依赖方向应从入口和应用层指向平台能力，而不是反向依赖。

| 层 | 路径 | 职责 |
| --- | --- | --- |
| 传输层 | `internal/transport/restapi` | HTTP 路由、认证入口、SSE 响应和请求编排 |
| 应用层 | `internal/application` | Agent 组装、上下文管理、工具中间件、知识库用例（`knowledge`） |
| 领域能力 | `internal/rag`、`internal/tool`、`internal/skill` | 正文读写、解析/切块/向量存储、工具实现与 Skill 加载 |
| 平台层 | `internal/platform` | 配置、认证、执行器、可观测性、隐私、队列与持久化 |

依赖方向是单向的：传输层 → 应用层 → 领域能力/平台层。应用层对平台能力的引用走
**接口**（port），实现放在平台层并由组合根注入 —— 例如
`application/knowledge.IndexTaskQueue` 由 `platform/queue/asynq.IndexQueue` 实现，
所以适配器不需要反向 import 应用层。

进程有两个组合根，各自装配自己需要的那一半依赖：

| 组合根 | 装配 |
| --- | --- |
| `cmd/restapi` | 配置、ent、工具注册表、Agent Harness、知识库用例（含**投递口**）、HTTP 服务 |
| `cmd/worker` | 配置、ent、正文存储、embedding、Milvus、`knowledge.Indexer`、asynq **消费端** |

`cmd/ragserver` 是本地跑 RAG 链路用的调试入口，不属于生产运行形态。

## Agent 结构

```text
HTTP /api/v1/chat
        |
   root_agent
  /     |      \
knowledge workspace automation
```

| Agent | 可用能力 | 边界 |
| --- | --- | --- |
| `root_agent` | 调用专项 Agent | 只路由，不直接读取、写入或执行 |
| `knowledge_agent` | `search_knowledge` | 仅返回授权知识库内容和引用 |
| `workspace_agent` | `read_file`、`list_dir` | 只读工作区 |
| `automation_agent` | `write_file`、可选 `shell` | 受工具白名单和审批策略控制 |

专项 Agent 定义于 `internal/application/agent`。根 Agent 使用 `adk.NewAgentTool` 包装专项 Agent；工具注册表位于 `internal/tool/registry`，工具本身必须先注册、再显式授予目标 Agent。

## 请求生命周期

1. `POST /api/v1/chat` 经过 API Key 认证，创建或恢复会话。
2. 服务写入用户消息和 `ChatTurn`，准备有限长度的历史上下文。
3. Eino Runner 流式运行根 Agent；响应使用 SSE 返回内容、审批请求或错误事件。
4. 工具调用经过 `middleware.Policy`：不在 `security.allowedTools` 的工具被拒绝；受保护写入或 shell 操作创建审批并中断运行。
5. 审批人调用 decision API 后，调用 resume API 恢复检查点；原始参数哈希必须匹配，防止审批后替换操作。
6. 回答完成后，服务保存助手消息并关闭本轮运行状态。

## 配置与环境变量

默认配置文件为 `configs/config.yaml`，可通过 `EINO_CONFIG` 覆盖。`cmd/restapi` 还会
额外读一份传输层配置 `internal/transport/restapi/etc/restapi.yaml`（`EINO_REST_CONFIG`
可覆盖），里面只放监听地址、请求体上限、超时和日志；业务配置一律走前者。支持以下
覆盖项：

| 环境变量 | 用途 |
| --- | --- |
| `EINO_MODEL_API_KEY` | 对话模型 API Key |
| `EINO_MODEL_BASE_URL` | 对话模型 Base URL |
| `EINO_MODEL` | 对话模型名称 |
| `EINO_SERVER_PORT` | HTTP 端口 |
| `EINO_WORKSPACE_ROOT` | 工作区根目录 |
| `EINO_EMBEDDING_API_KEY` | Embedding API Key，名称由 `embedding.apiKeyEnv` 配置；只有 `cmd/worker` 需要 |
| `EINO_STORAGE_PASSWORD` | PostgreSQL 密码，名称由 `storage.passwordEnv` 配置 |
| `EINO_API_KEY_DEVELOPER` | Agent 调用 API Key |
| `EINO_API_KEY_APPROVER` | 审批 API Key |
| `EINO_API_KEY_ADMIN` | 管理 API Key |

Redis 口令同理走 `asynq.redis.passwordEnv`，两个进程都要能读到同一个值。

不要将 API Key、数据库密码或生产地址提交到配置文件。`storage.passwordEnv` 必须指向一个已设置的环境变量。

## HTTP API

| 方法 | 路径 | 角色 | 用途 |
| --- | --- | --- | --- |
| `GET` | `/health` | 无 | 存活检查 |
| `GET` | `/ready` | 无 | 数据库就绪检查 |
| `POST` | `/api/v1/sessions` | `agent` | 创建会话 |
| `POST` | `/api/v1/chat` | `agent` | 发起流式对话（SSE） |
| `GET` | `/api/v1/approvals/{id}` | `approver` | 查询审批 |
| `POST` | `/api/v1/approvals/{id}/decision` | `approver` | 批准或拒绝 |
| `POST` | `/api/v1/approvals/{id}/resume` | `agent` | 恢复已批准运行（SSE） |
| `POST` | `/api/v1/dataset` | `admin` | 创建数据集；`name`、可选 `description` 和 `visibility`（`private` 或 `system`） |
| `GET` | `/api/v1/dataset` | `admin` | 列出数据集 |
| `GET` | `/api/v1/dataset/{id}` | `admin` | 获取数据集详情 |
| `DELETE` | `/api/v1/dataset/{id}` | `admin` | 删除数据集；成功返回 `{"status":"deleted"}` |
| `POST` | `/api/v1/dataset/{id}/documents` | `admin` | 创建文档；`content` 落 `knowledge.root` 托管文件，只给 `source` 则注册既有文件。返回 `status: indexing` |
| `GET` | `/api/v1/dataset/{id}/documents` | `admin` | 列出数据集下的文档，含分块数与已索引分块数 |
| `GET` | `/api/v1/dataset/{id}/documents/{docId}` | `admin` | 获取文档详情 |
| `PUT` | `/api/v1/dataset/{id}/documents/{docId}` | `admin` | 更新文档；给 `content` 会改写正文并重新索引 |
| `DELETE` | `/api/v1/dataset/{id}/documents/{docId}` | `admin` | 删除文档，一并清理分块、索引任务与向量；返回 `{"status":"deleted"}` |
| `POST` | `/api/v1/dataset/{id}/documents/{docId}/reindex` | `admin` | 重建单个文档索引 |
| `POST` | `/api/v1/dataset/{id}/reindex` | `admin` | 重建数据集全部文档索引；返回受理计数，不等待索引完成 |
| `GET` | `/api/v1/agents/{subject}/dataset` | `admin` | 查询 API Key 主体可访问的数据集 |
| `PUT` | `/api/v1/agents/{subject}/dataset/{id}` | `admin` | 为 API Key 主体授权一个启用中的数据集；成功与重复授权都是空体 204 |
| `DELETE` | `/api/v1/agents/{subject}/dataset/{id}` | `admin` | 撤销授权；成功返回空体 204 |

文档的写操作**不同步等索引**，而且请求内**不切块**。HTTP 侧只做三件事：

1. 把正文落到 `knowledge.root` 下的托管文件；
2. 写下 `documents` 行，`status=indexing`；
3. 把「这篇文档要索引」投进 Redis 队列（`knowledge:index` / 队列 `index`）。

切块、embedding 和写向量全部在 `cmd/worker` 进程里完成。所以响应返回时 `status`
是 `indexing`，而 `chunk_count` 与 `indexed_chunk_count` 都是**实时聚合值**（从
`document_chunks` 现算，`documents` 表不存冗余计数）：

- 刚创建时两者都是 `0`，不代表文档为空 —— 只是 worker 还没跑到。客户端不要用
  `chunk_count == 0` 判空文档；
- `chunk_count` 由 worker 切块时逐批写入而增长；`chunk_count` 追平且该文档所有
  chunk 都是 `indexed` 后，`status` 变 `ready`；
- 重试耗尽仍失败的 chunk 会把文档置为 `failed`。所以 `status != ready` 也不等于
  失败，要看具体是 `indexing` 还是 `failed`。

投递失败是**同步失败**：队列不可用（Redis 连不上、`asynq.enabled=false`）时请求直接
返回错误，`documents` 行被标记为 `failed`，不会留下一个永远停在 `indexing` 的孤儿。

路由与类型由 `internal/transport/restapi/docs/*.api` 驱动 goctl 生成，是契约的唯一来源。
指标不再暴露在同端口：go-zero 自带 Prometheus agent 在
`internal/transport/restapi/etc/restapi.yaml` 的 `Prometheus.Host/Port` 上独立监听
（当前该段是注释状态，即指标未暴露）。

## 知识库数据流

摄取与索引跨两个进程，唯一的连接是 Redis 队列和 `platform/queue/tasks` 里的任务契约：

```text
【cmd/restapi · HTTP 进程】只做「接收 + 落库 + 投递」，不碰 embedding 和向量库
  上传正文 → ContentStore 写 knowledge.root 托管文件
           → INSERT documents (status=indexing)
           → Enqueue(knowledge:index, {dataset_id, document_id})
                    │
      ══════════════╎══════════════  Redis 队列 index（asynq broker）
                    │              payload: KnowledgeIndexPayload
【cmd/worker · 消费进程】承担全部重活，PostgreSQL / Milvus / embedding 只在这里构造
  HandleTask → rag.Pipeline 的 ingest 链（与 cmd/ragserver 同一份实现）
                 FileLoader 按 source 读回正文
                 → Parser 拆出 N 个产品块（当前是 front-matter 分节）
                 → MarkdownChunker 切块
             → INSERT document_chunks (vector_status=pending)
             → Embedder 批量向量化 → Milvus upsert
             → chunk: pending→indexed；全部追平后 document: indexing→ready
             → 重试耗尽仍失败 → chunk: failed，document: failed

【查询】仍在 HTTP 进程内（未拆分）
  型号精确检索 + 向量检索 + PostgreSQL 关键词检索 → RRF 融合
    → ACL 过滤 → 含引用的 search_knowledge 输出
```

拆分的收益是故障域隔离：embedding 或 Milvus 慢/挂只影响消费进度，不影响 HTTP 收发；
HTTP 重启也不会丢在跑的任务。代价是多了一个必须独立部署、独立配队列的进程，运维上
两个进程要能看到同一份 `asynq` 配置和同一个 Redis。

重入靠两道幂等：`documents.metadata.content_hash`（正文 SHA-256）让「内容没变的重建
索引」跳过重新切块；`embedPending` 每轮只挑 `vector_status=pending` 的 chunk，所以
重试是**按 chunk 收敛**的，已经写进 Milvus 的块不会重做。因此在跑的任务被取消
（关闭超时、进程退出）时**不能**写终态 —— 它会被退回队列重跑，写终态就会把一次正常的
重试变成永久失败。

文档和切块记录保存在 PostgreSQL；Milvus 仅保存 `chunk_id` 与向量。最终查询会再次按文档可见性过滤，因此向量库的候选结果不能直接暴露给用户。

文档正文以文件为唯一真相，存在 `knowledge.root` 下：接口创建的文档放在
`documents/` 托管子目录里（`documents.source` 保存相对路径），调用方自己放进
root 的文件则以 `source` 注册、只读不改。因此「重建索引」就是把文件重新读出来，
数据库里不再存第二份正文。

| 模块 | 单一职责 | 不负责 |
| --- | --- | --- |
| `rag.ContentStore` | 在 knowledge root 内安全读写正文文件（含软链与路径穿越校验） | 文档归属、切块、索引 |
| `rag.FileLoader` | 在受控目录内安全读取支持的文本文件 | 知识库归属、权限和入库 |
| `rag.Pipeline` | 把「读入 → 按产品拆分 → 切块」串成一条 eino Chain；`Config.Parser` 是拆分规则的唯一接缝，`Config.Store` 为 nil 时链尾就停在切块（worker 用的就是这一档） | 文档归属、分块状态机、检索授权 |
| `application/knowledge.Service` | 校验入参、落正文文件、写 `documents` 行、提交后投递索引任务；文档增删改查编排与实时分块统计 | 切块、embedding、写向量 |
| `knowledge.Indexer` | 消费索引任务：驱动 `rag.Pipeline` 拿分块 → 落 pending 行 → embedding → 写向量，并维护 chunk/document 的索引状态 | HTTP 入参校验、检索排序 |
| `platform/queue.Producer` | 把「这篇文档要索引」投进 Redis 队列（HTTP 侧持有的抽象） | 决定索引策略、写向量、消费 |
| `platform/queue/asynq.IndexQueue` | 把 `Producer` 适配成应用层的 `knowledge.IndexTaskQueue` 端口 | 业务决策 |
| `platform/queue/asynq.AsynqClient` | 同时是 asynq 的 Producer 与 Consumer（后者实现 `service.Service`，由 `cmd/worker` 单独启停） | 业务处理与存储访问 |
| `retrieval.HybridRetriever` | 编排型号、关键词和向量召回，统一授权过滤与融合排序 | 直接对外格式化回答 |
| `tool.KnowledgeSearch` | 将经过认证的主体和服务端 KB 白名单转换为检索请求并格式化引用 | 让模型决定可访问的知识库 |

`application/knowledge` 是这条链路上的**唯一业务入口**：HTTP transport 只认识
`Service`，不认识队列；`Indexer` 只认识任务 payload，不认识 HTTP。两边各自依赖
`Splitter` / `ContentStore` / `VectorIndex` 这些接口，具体实现在组合根里接线。

### 进程编排

`cmd/restapi` 与 `cmd/worker` 各自只挂一个 `service.Service` 到 `service.ServiceGroup`：

| 进程 | 挂载的 service | 关闭预算来自 |
| --- | --- | --- |
| `cmd/restapi` | `transport/restapi.Server`（HTTP 监听） | `internal/transport/restapi/etc/restapi.yaml` 的 `Shutdown.WaitTime` |
| `cmd/worker` | `asynq.AsynqClient`（消费端，`Start()` 阻塞到 `Stop()`） | `configs/config.yaml` 的 `asynq.shutdownTimeoutSeconds` |

HTTP 进程有一个容易踩的细节：监听的关闭**不由** `Stop()` 触发。go-zero 把监听的关闭
挂在 `core/proc` 的全局 shutdown 链上（SIGTERM/SIGINT 触发），而那条链只能被 notify
一次 —— listener 内部会 `WaitGroup.Done`，再来一次就 panic —— 所以 `Stop()` 只负责等
监听退出，关闭权交给 proc。`Shutdown.WaitTime` 必须大于本次请求链路上最慢的一次
同步操作，否则还没有响应完的请求会被 go-zero 直接 `Kill` 掉（默认预算只有 5.5s）。

worker 进程的关闭预算含义不同：它等的是**正在跑的索引任务**排空。任务可能正卡在
embedding 的网络往返上，`asynq.shutdownTimeoutSeconds` 要按任务实际耗时配；配小了
超时的任务会被退回队列重跑，白烧一次 embedding（只是浪费，不会错 —— 靠
`content_hash` 与 pending 语义收敛）。

两个进程都拒绝「半个配置」启动：HTTP 在 `asynq.enabled=false` 时仍可启动（只是文档
写操作会同步失败），worker 则直接拒绝启动，因为一个没有队列可消费的 worker 只会空转。

知识库、文档和授权关系由 `internal/transport/restapi` 的 admin 接口维护
（契约见 `internal/transport/restapi/docs/dataset/dataset.api` 与
`admin/admin.api`）；领域逻辑在 `internal/application/knowledge`，切块、正文
文件与向量基础设施在 `internal/rag`。`agent_datasets` 以 API Key 的 `subject`
为主体保存可访问 dataset ID；`search_knowledge` 在每次调用时读取这些绑定，
因此未绑定主体绝不会检索任何资料，模型也不能通过工具参数扩大范围。
`Metadata` 在入队时统一合并文档字段、分块序号和标题路径，避免各调用方直接
修改持久化元数据。`SearchScope` 的知识库 ID 是强制白名单：空列表表示无检索
权限，绝不会回退为全库搜索。

当前系统只有固定内部执行器 `knowledge_agent`；它代表发起请求的 API Key 主体运行。未来如加入可持久化的 Agent 实体，可将绑定主体从 `subject` 扩展为 Agent ID。

本地上传、分块、索引和检索的验证步骤见 [RAG 本地验证](rag-testing.md)。

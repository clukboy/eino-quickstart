# 架构与运行说明

## 分层

项目按职责分为四层，依赖方向应从入口和应用层指向平台能力，而不是反向依赖。

| 层 | 路径 | 职责 |
| --- | --- | --- |
| 传输层 | `internal/transport/httpapi` | HTTP 路由、认证入口、SSE 响应和请求编排 |
| 应用层 | `internal/application` | Agent 组装、上下文管理、工具策略和输出限制 |
| 领域能力 | `internal/knowledge`、`internal/tool`、`internal/skill` | 知识库、工具实现与 Skill 加载 |
| 平台层 | `internal/platform` | 配置、认证、执行器、可观测性、隐私和持久化 |

`cmd/server` 是组合根：它读取配置、构造平台依赖、注册工具，最后创建 HTTP 服务。`cmd/maintenance` 独立执行过期审批、检查点和对话轮次的清理。

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

默认配置文件为 `configs/config.yaml`，可通过 `EINO_CONFIG` 覆盖。支持以下覆盖项：

| 环境变量 | 用途 |
| --- | --- |
| `EINO_MODEL_API_KEY` | 对话模型 API Key |
| `EINO_MODEL_BASE_URL` | 对话模型 Base URL |
| `EINO_MODEL` | 对话模型名称 |
| `EINO_SERVER_PORT` | HTTP 端口 |
| `EINO_WORKSPACE_ROOT` | 工作区根目录 |
| `EINO_EMBEDDING_API_KEY` | Embedding API Key |
| `EINO_STORAGE_PASSWORD` | PostgreSQL 密码，名称由 `storage.passwordEnv` 配置 |
| `EINO_API_KEY_DEVELOPER` | Agent 调用 API Key |
| `EINO_API_KEY_APPROVER` | 审批 API Key |
| `EINO_API_KEY_ADMIN` | 管理 API Key |

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

文档的写操作**不同步等索引**：请求内只做切块与落库，返回的 `status` 是
`indexing`，`chunk_count` 已是最终值，`indexed_chunk_count` 由后台 worker
逐渐追上，追平后 `status` 变 `ready`；重试耗尽则变 `failed`。客户端不要用
`status != ready` 判定失败。

路由与类型由 `internal/transport/restapi/docs/*.api` 驱动 goctl 生成，是契约的唯一来源。
指标不再暴露在同端口：go-zero 自带 Prometheus agent 在 `etc/restapi.yaml` 的
`Prometheus.Host/Port` 上独立监听（当前该段是注释状态，即指标未暴露）。

## 知识库数据流

设计上的流程是：

```text
文档 -> Chunker -> PostgreSQL documents/document_chunks
     -> asynq 队列(index) -> Embedder -> Milvus
查询 -> 型号精确检索 + 向量检索 + PostgreSQL 关键词检索 -> RRF 融合
     -> ACL 过滤 -> 含引用的 search_knowledge 输出
```

文档和切块记录保存在 PostgreSQL；Milvus 仅保存 `chunk_id` 与向量。最终查询会再次按文档可见性过滤，因此向量库的候选结果不能直接暴露给用户。

文档正文以文件为唯一真相，存在 `knowledge.root` 下：接口创建的文档放在
`documents/` 托管子目录里（`documents.source` 保存相对路径），调用方自己放进
root 的文件则以 `source` 注册、只读不改。因此「重建索引」就是把文件重新读出来，
数据库里不再存第二份正文。

| 模块 | 单一职责 | 不负责 |
| --- | --- | --- |
| `rag.ContentStore` | 在 knowledge root 内安全读写正文文件（含软链与路径穿越校验） | 文档归属、切块、索引 |
| `rag.FileLoader` | 在受控目录内安全读取支持的文本文件 | 知识库归属、权限和入库 |
| `application/knowledge.Service` | 校验入参、生成分块/元数据，在事务里写入文档与 chunk，提交后投递索引任务；文档增删改查的编排 | 文件系统扫描、向量生成 |
| `knowledge.Indexer` | 消费 asynq 索引任务，调用 embedding 与向量库，并维护 chunk/document 的索引状态 | 文档解析、检索排序 |
| `platform/queue.Client` | 把「这篇文档要索引」投进 Redis 队列 | 决定索引策略、写向量 |
| `platform/queue.Server` | 把 asynq worker 适配成 go-zero 的 `service.Service`，与 HTTP 一起交给 `ServiceGroup` 启停 | 业务处理与存储访问 |
| `retrieval.HybridRetriever` | 编排型号、关键词和向量召回，统一授权过滤与融合排序 | 直接对外格式化回答 |
| `tool.KnowledgeSearch` | 将经过认证的主体和服务端 KB 白名单转换为检索请求并格式化引用 | 让模型决定可访问的知识库 |

服务编排：`cmd/restapi` 用 go-zero 的 `service.ServiceGroup` 同时挂两个服务 ——
HTTP 监听（`transport/restapi.Server`）与 asynq worker（`platform/queue.Server`）。
两者都实现「`Start()` 阻塞到 `Stop()`」这条约定，所以进程生命周期只有一处负责。
但要注意 HTTP 的关闭**不由** `Stop()` 触发：go-zero 把监听的关闭挂在 `core/proc`
的全局 shutdown 链上（SIGTERM/SIGINT 触发），而那条链只能被 notify 一次 ——
listener 内部会 `WaitGroup.Done`，再来一次就 panic —— 所以 `Stop()` 只负责等监听
退出，关闭权交给 proc。进程关闭的总预算由 `etc/restapi.yaml` 的 `Shutdown.WaitTime`
控制，它必须大于 `asynq.shutdownTimeoutSeconds`，否则还没排空完的进程会被 go-zero
直接 `Kill` 掉（默认预算只有 5.5s）。

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

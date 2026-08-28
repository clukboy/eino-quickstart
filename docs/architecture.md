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
| `GET` | `/metrics` | `admin` | Prometheus 指标；仅在 `metricsEnabled: true` 时注册 |
| `POST` | `/api/v1/sessions` | `agent` | 创建会话 |
| `POST` | `/api/v1/chat` | `agent` | 发起流式对话 |
| `GET` | `/api/v1/approvals/{id}` | `approver` | 查询审批 |
| `POST` | `/api/v1/approvals/{id}/decision` | `approver` | 批准或拒绝 |
| `POST` | `/api/v1/approvals/{id}/resume` | `agent` | 恢复已批准运行 |

认证使用请求中的 API Key。响应与 SSE 事件的字段定义见 `internal/transport/httpapi/server.go`。

## 知识库数据流

设计上的流程是：

```text
文档 -> Chunker -> PostgreSQL documents/document_chunks
     -> vector_outbox -> Embedder -> Milvus
查询 -> 向量检索 + PostgreSQL 关键词检索 -> RRF 融合
     -> ACL 过滤 -> 含引用的 search_knowledge 输出
```

文档和切块记录保存在 PostgreSQL；Milvus 仅保存 `chunk_id` 与向量。最终查询会再次按文档可见性过滤，因此向量库的候选结果不能直接暴露给用户。

本地上传、分块、索引和检索的验证步骤见 [RAG 本地验证](rag-testing.md)。

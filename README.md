# Eino Harness

基于 [Eino](https://github.com/cloudwego/eino) ADK 的多 Agent 软件工程 Harness。入口服务位于 `cmd/server`，根协调 Agent 会将请求分派给知识库、工作区读取和受控自动化专项 Agent。

> **项目状态：开发中。** 在首次部署前，请阅读 [待完善项](docs/known-gaps.md)；其中的知识库启动接线尚未完成，当前服务不能正常构造 `knowledge_agent`。

## 文档

- [架构与运行说明](docs/architecture.md)：分层结构、请求生命周期、启动前配置与 HTTP API。
- [新增 Agent、工具与 Skill](docs/agent-development.md)：专项 Agent 的接入步骤、Eino Tool 编写方式、Skill 配置与编写规范。
- [待完善项](docs/known-gaps.md)：未完成能力、已知限制与完成标准。

## 主要目录

| 路径 | 职责 |
| --- | --- |
| `internal/application` | 根协调器、专项 Agent、上下文与工具中间件 |
| `internal/tool` | 工具实现、注册表与风险分级 |
| `internal/skill` | `SKILL.md` 发现与按需加载 |
| `internal/platform` | 认证、配置、执行、可观测性与持久化 |
| `internal/knowledge` | 文档处理、嵌入、检索与向量存储 |
| `internal/transport/httpapi` | HTTP API 与 SSE 传输层 |
| `configs/config.yaml` | 服务、模型、工作区、Skill 和安全配置 |

## 快速启动

1. 准备 PostgreSQL、Milvus 和执行器镜像；配置中的地址仅为示例值。
2. 设置必填环境变量：

   ```bash
   export EINO_MODEL_API_KEY=...
   export EINO_EMBEDDING_API_KEY=...
   export EINO_STORAGE_PASSWORD=...
   export EINO_API_KEY_DEVELOPER=...
   export EINO_API_KEY_APPROVER=...
   export EINO_API_KEY_ADMIN=...
   ```

3. 根据部署环境修改 `configs/config.yaml`，特别是 `storage`、`milvus`、`execution` 和 `observability`。
4. 在完成 [知识库启动接线](docs/known-gaps.md#p0-知识库启动接线) 后启动：

   ```bash
   go run ./cmd/server
   ```

服务默认监听 `:8080`，健康检查为 `GET /health`。

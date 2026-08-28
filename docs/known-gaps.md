# 待完善项

本文只记录已确认会影响生产部署或功能边界的事项。RAG 文件扫描、分块、outbox、Embedding 与 Milvus 索引 worker 已实现；其本地验证流程见 [RAG 本地验证](rag-testing.md)。

## P1：知识库 HTTP 上传接口

**状态：未实现**

当前知识库通过 `knowledge.root` 的受控目录摄取文件。将 Markdown 复制到该目录后运行 `cmd/knowledge-worker` 即可完成索引，但服务尚未提供 `multipart/form-data` 上传 API。

完成标准：

1. 仅允许已授权主体上传支持的 UTF-8 文本格式。
2. 在服务端实施文件大小、文件名和路径边界校验。
3. 上传后调用摄取服务，并返回文档 ID、切块数量和索引状态。
4. 对私有文档保存上传者为 `owner_subject`，并覆盖 ACL 与审计测试。

## P1：Skill 未授予默认 Agent

**状态：设计待决**

启动过程会注册 `list_skills` 和 `load_skill`，但默认专项 Agent 未取得它们。因此 `skills/` 中的内容不会被默认 Agent 实际加载。

完成标准：

1. 明确哪些专项 Agent 可以使用 Skill。
2. 将所需 Skill 工具加入对应的 `registry.Require(...)`。
3. 对受策略保护的 Agent，同步更新 `security.allowedTools`。
4. 添加 Agent 路由测试。

## P1：生产数据库迁移策略

**状态：开发模式实现**

`internal/platform/storage/entx/database.go` 会在启动时调用 `client.Schema.Create`。这适合学习和本地开发；生产环境应采用受版本控制的 Ent/Atlas migration，避免多实例启动时改表。

## P2：运行状态与可观测性闭环

**状态：需补充验证**

运行、审批、对话轮次和 tracing 已有基础实现，但仍缺少覆盖中断、恢复、异常和超时路径的端到端测试；OTLP provider 也尚未在服务组合根中创建与关闭。

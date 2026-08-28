# 待完善项

本文仅记录当前代码中已确认、会影响部署或能力完整性的事项。每项均给出影响范围与完成标准，避免将计划性优化误认为已完成能力。

## P0：知识库启动接线

**状态：未完成**

`internal/application/agent/factory.go` 要求工具注册表中存在 `search_knowledge`，但 `cmd/server/main.go` 尚未创建 Embedder、Milvus Store、Hybrid Retriever 或知识搜索工具并注册。因此服务器会在 `agent.NewHarness` 阶段因缺少 `search_knowledge` 失败。

完成标准：

1. 从 `embedding`、`milvus`、`knowledge` 和 `retrieval` 配置创建依赖。
2. 创建并检查 Milvus collection。
3. 构造 `retrieval.HybridRetriever` 和 `tool.NewKnowledgeSearch`，随后注册该工具。
4. 启动时关闭 Milvus Store，并为其失败提供清晰日志。
5. 添加使用 fake 依赖的组合根测试，验证工具已注册且 `NewHarness` 可创建。

## P0：知识文件发现与向量索引 Worker

**状态：未完成**

`knowledge.Service.Ingest` 已能写入文档、切块和 `vector_outbox`，但 `internal/knowledge/loader.go` 仍为空，且没有 worker 消费 outbox、调用 Embedding API、写入 Milvus、更新 `vector_status`。即使手动调用摄取，查询也不会得到已索引内容。

完成标准：

1. 实现受根目录和最大文件大小限制的知识文件扫描器，拒绝越界路径和符号链接逃逸。
2. 在启动或独立命令中调用扫描器与 `Service.Ingest`。
3. 实现 outbox worker：批量领取待处理记录、嵌入 chunk、写入/删除 Milvus 向量，并以事务更新 chunk 和 outbox 状态。
4. 为失败实现有限重试、退避和可观测日志，避免永久卡住 `processing` 记录。
5. 添加 PostgreSQL/Milvus 集成测试或容器化测试。

## P1：Skill 未授予任何默认 Agent

**状态：设计待决**

启动过程已注册 `list_skills` 和 `load_skill`，但 `knowledge_agent`、`workspace_agent` 和 `automation_agent` 的工具集均未包含它们。因此当前 `skills/` 中的内容不会被任何 Agent 实际加载。

完成标准：

1. 明确哪些专项 Agent 可以使用 Skill。
2. 将 `list_skills`、`load_skill` 加入这些 Agent 的 `registry.Require(...)`。
3. 对受策略保护的 Agent，同步加入 `security.allowedTools`。
4. 在 Agent 指令中要求先列举、后按需加载，并添加路由测试。

## P1：生产数据库迁移策略

**状态：开发模式实现**

`internal/platform/storage/entx/database.go` 每次启动调用 `client.Schema.Create`。这适合学习或本地开发，但生产部署应使用受版本控制的 Ent/Atlas migration，避免实例启动时并发改表。

完成标准：

1. 生成并审查版本化迁移文件。
2. 在部署流程中单独执行迁移。
3. 将应用启动改为仅连接和健康检查。

## P2：运行状态与可观测性闭环

**状态：需补充验证**

运行、审批、对话轮次和 tracing 已有基础实现，但缺少覆盖中断、恢复、异常和超时的端到端测试。OTLP provider 也尚未在服务组合根中创建和关闭。

完成标准：

1. 在 `cmd/server` 创建 `NewTracerProvider` 并在关闭阶段调用 `Shutdown`。
2. 覆盖 chat、审批、恢复、失败、取消和超时路径的集成测试。
3. 验证 `AgentRun` 与 `ChatTurn` 在全部终止路径都有准确状态。

## P2：配置分环境管理

**状态：基础配置已提供**

配置含有开发环境默认值和网络地址，不应直接作为生产配置复用。密码已通过 `storage.passwordEnv` 转为环境变量，但其余服务地址、镜像、日志目录与 tracing 端点仍需要按环境管理。

完成标准：

1. 提供不含环境地址和密钥的示例配置。
2. 使用部署平台的环境变量或密钥管理器注入生产值。
3. 在 CI 中验证配置完整性，但不输出敏感值。

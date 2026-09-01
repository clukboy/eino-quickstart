# 待完善项

本文只记录已确认会影响生产部署或功能边界的事项。RAG 文件扫描、分块、outbox、Embedding 与 Milvus 索引 worker 已实现；其本地验证流程见 [RAG 本地验证](rag-testing.md)。

## P0：检索质量评测与发布门禁

**状态：基础数据集已具备，尚未接入发布流程**

当前 `internal/eval/datasets/` 已有检索用例和阈值，但不会在部署前自动执行，也没有持续跟踪 Recall@K、无答案准确率、引用正确性、ACL 泄漏数和 P95 延迟。

完成标准：

1. 建立覆盖产品型号、同义表达、跨文档、无答案和私有文档越权场景的版本化金标集。
2. 在 CI 或发布流水线执行 `cmd/rag-test` 的非交互评测模式，并以 `thresholds.yaml` 阻断不达标发布。
3. 按 embedding 模型、切块策略和检索配置记录对比结果，避免参数改动造成隐性质量回归。

## P1：Rerank 接入

**状态：未实现**

`retrieval.enableRerank` 和 `Reranker` 接口已经预留，但 `HybridRetriever` 尚未接收或调用 reranker；当前配置为 `true` 不会改变最终排序。

完成标准：

1. 接入一个可配置的 reranker，在 ACL 过滤后对有限候选集重排。
2. 明确超时、降级和错误策略，并记录 rerank 前后排名以便诊断。
3. 增加重排生效、超时降级和私有文档不泄漏的测试。

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

## P2：检索可观测性与文档生命周期

**状态：基础日志已具备，缺少运营闭环**

Indexer 会记录批处理数量和失败原因，但没有按文档、检索通道和查询结果提供指标或管理入口；已删除或长期失败的文档也不能通过 API 重新索引和修复。

完成标准：

1. 暴露摄取量、outbox 积压、重试/失败数、向量/关键词/型号通道命中数和检索延迟指标。
2. 提供按文档查询状态、重试失败索引、删除文档及清理对应向量的受权管理能力。
3. 为 embedding 模型、维度或集合变更设计全量重建和版本切换流程。

## P1：生产数据库迁移策略

**状态：开发模式实现**

`internal/platform/storage/entx/database.go` 会在启动时调用 `client.Schema.Create`。这适合学习和本地开发；生产环境应采用受版本控制的 Ent/Atlas migration，避免多实例启动时改表。

## P2：运行状态与可观测性闭环

**状态：需补充验证**

运行、审批、对话轮次和 tracing 已有基础实现，但仍缺少覆盖中断、恢复、异常和超时路径的端到端测试；OTLP provider 也尚未在服务组合根中创建与关闭。

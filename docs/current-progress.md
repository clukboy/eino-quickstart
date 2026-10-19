# Eino Harness 当前开发进度

> 整理日期：2026-10-07（Asia/Shanghai）
> 阅读范围：当前后端仓库的入口程序、应用层、RAG、工具、持久化、REST API 契约及测试。  
> 源码基线：HEAD `8887fce`，提交日期 2026-09-29，提交说明「用户列表支持昵称与关键字搜索」；**同时计入当前工作区尚未提交的源码变更与新增文件**。  
> 本文是源码进度快照，不是上线验收报告；未对独立前端仓库或真实外部服务完成情况作背书。
> 后续 P02 配置拆分进展见第 10 节；最新启动配置以 [P02 配置指南](p02-configuration.md) 为准。

## 1. 总体结论

**项目已从 Quickstart 演进为具备主要业务链路的多 Agent 后端，当前处于功能收敛、联调验证和生产化补齐阶段。**

已经形成的主链路：

1. 多 Agent 对话：协调 Agent 分派知识问答、工作区读取和受控操作，响应通过 SSE 输出。
2. 会话与审批：保存历史和执行状态，支持操作中断、审批决策、检查点续跑。
3. 知识库写入：数据集与文档管理、JSON 正文创建、multipart 多文件上传、产品文档拆分与增量更新。
4. 异步索引：HTTP 进程投递 Redis/asynq 任务，独立 worker 完成切块、embedding、Milvus 与可选 ES 索引写入。
5. 知识召回：精确、关键词、向量三通道，加权 RRF 融合，HTTP 召回与对话工具共用装配链路。
6. 管理接口：账号列表与建号、知识库主体绑定、审批列表等已具备源码实现。

**不能认定已经全部完成：** LLM 产品拆分的真实模型质量、外部用户同步，以及真实 PostgreSQL/Redis/worker/模型/Milvus/ES/OTLP 联调仍有明确边界；P01-P17（其中 P16/P17 为本地契约）、P18、P20-P29 已形成不同程度的本地可复跑验收，但本地契约不能替代真实外部依赖和多实例演练。

本仓库没有完整的需求总表、工作量权重或上线验收结果，因此不提供容易误导的“整体完成百分比”，以下按可核对的模块状态汇总。

## 2. 完成度总览

状态口径：

- **已实现**：存在主要业务实现及入口接线，不等于已通过真实环境验收。
- **部分完成**：基础功能已有，仍有扩展或运行闭环缺口。
- **待实现**：当前只有接口、配置、占位，或未找到对应实现。
- **待验证**：本次未执行依赖真实服务的联调或验收。

| 模块 | 当前状态 | 已有进展 | 剩余边界 |
| --- | --- | --- | --- |
| 后端分层与入口 | 已实现 | 应用层、平台层、传输层分离；HTTP 与索引 worker 独立进程 | 部署与运维验收待补 |
| 多 Agent 编排 | 已实现 / 待验证 | Root + Knowledge + Workspace + Automation；工具按职责分配 | 尚未做真实模型端到端验证 |
| SSE 对话与历史 | 已实现 | 流式消息、结束/错误/审批事件；会话、轮次和运行记录持久化 | 异常、超时及多实例场景仍需联调 |
| 审批与检查点续跑 | 已实现 / 待验证 | 列表、分页、状态筛选、详情、决策、恢复与重复续跑保护 | 真实执行与异常恢复验收待补 |
| 工作区工具 | 已实现 | 文件读取、目录列表、写入；执行器支持 disabled/local/docker | 默认 Skill 能力未授予专项 Agent |
| 账号与用户管理 | 部分完成 | 匿名身份、登录、首次改密、管理员建号、昵称和关键字查询；P19 已交付 provider-neutral 同步本地契约 | 真实外部用户源、管理生命周期与限流仍待验收 |
| 数据集管理 | 已实现 | 创建、列表、详情、删除、主体绑定及绑定反查 | 没有完整的数据集编辑/状态切换接口 |
| 文档管理与上传 | 已实现 / P12 本地验收通过 | CRUD、正文读取、启停、单文档/整库重建；multipart 支持 Markdown/文本多文件、总请求/单文件/文件数限制、逐文件失败台账和临时文件清理 | 仍是受限读取而非全链路流式；目录批量导入待补；真实依赖联调待补 |
| 产品文档拆分 | 部分完成 | front-matter 解析、一产品一文档、型号查重、正文与元数据指纹 | 未实现 LLM 自动识别产品边界 |
| 异步索引 | 已实现 / 待验证 | Redis/asynq、独立 worker、分批 embedding、状态推进与重试收敛 | 真实故障注入和恢复验收待补 |
| 混合召回 | 已实现 | 精确 + BM25/PG 回落 + 向量；加权 RRF、通道降级信息 | 无 ES 时精确通道缺席；Rerank 未接入 |
| 对话知识库工具 | 已实现 | 按服务端主体绑定查询数据集，跨库结果合并、来源引用 | 无授权/依赖故障有明确返回；实际回答质量待测 |
| 检索质量评测 | 部分完成 | 17 条金标用例、指标计算、阈值配置和退出码门禁 | 未接 CI、缺跨版本留档与完整私有语料评测 |
| 运行可观测性 | 部分完成 | 结构化日志、追踪接线、健康接口、go-zero 指标接线；P01 已显式启用独立 Prometheus 端口并通过全量回归 | 业务指标、告警与生产采集策略仍待补 |
| 数据保留与清理 | 部分完成 | 清理过期审批、检查点和终态轮次的 Worker 已编写 | 未被入口进程启动 |
| 生产化 | 部分完成 | 配置校验、worker 依赖校验、退出处理等已存在 | 版本化迁移、索引版本切换、发布门禁与告警待补 |

## 3. 已完成能力详解

### 3.1 多 Agent 与工具体系

- `root_agent` 只做路由：每条请求必须委派一个专项 Agent，不直接回答；不直接承担专项工具操作。
- `knowledge_agent` 使用 `search_knowledge` 获取知识库内容并保留引用。
- `workspace_agent` 被授予 `read_file`、`list_dir`、`list_skills`、`load_skill`。
- `automation_agent` 被授予 `write_file`，执行模式非 disabled 时增加 `shell`。
- `general_agent` 是兜底专员：无工具、纯直答，处理不匹配其余三个专员的一切请求；模型可来自数据库配置的外部平台。除路由层兜底外，`ChatLogic` 还在一轮无产出时用它补跑一次，保证不会只回一个 `done`。
- 已实现工具注册、工具策略中间件、工具输出大小限制和会话上下文裁剪。
- `list_skills`、`load_skill` 已在启动时注册，但默认 Agent 的工具集合中没有它们，不能把“已注册”当成“默认 Agent 已使用 Skill”。

源码依据：[Agent 工厂](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/agent/factory.go)、[启动装配](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/restapi/main.go)、[上下文管理](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/contextmgr/manager.go)。

### 3.2 对话、会话与审批

- 已提供创建会话、会话列表和历史消息读取接口。
- 对话链路保存用户消息、最终回答、轮次与执行状态；支持按首条消息生成会话标题。
- SSE 使用 payload 的 `type` 区分事件；客户端不能只靠 HTTP 状态码判断对话成功与否。
- 审批链路已包含审批单生成、检查点关联、决策及恢复；续跑有认领与释放处理。
- 新增审批列表支持状态筛选、分页和总数，详情/决策/恢复并非只有路由占位。

源码依据：[对话逻辑](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/stream/chat_logic.go)、[续跑逻辑](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/stream/resume_approval_logic.go)、[审批列表](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/approver/list_approvals_logic.go)。

### 3.3 数据集、文档与产品摄取

- P05 已补齐产品型录的本地增量回归：三个 front-matter 产品拆成三条文档，按 `external_key` 保持稳定 ID；原样重传返回 `unchanged`，只修改一个产品返回 `updated`，重复型号明确返回校验错误。入口为 `bash scripts/acceptance/run.sh P05`。

- 数据集已有创建、列表、详情与删除；文档已有创建、读取、更新、删除、正文查询、启停、重建索引。
- 文档正文以 PostgreSQL 的 `documents.content` 为准，`source` 是逻辑溯源信息，不应当作为当前正文文件路径使用。
- multipart 上传**已经实现**，文件字段为 `file`，支持同一请求携带多个 Markdown/纯文本文件（`.md`、`.markdown`、`.txt`、`.text`）。
- P12 已将 `Knowledge.MaxUploadRequestBytes`、`Knowledge.MaxDocumentBytes`、`Knowledge.MaxUploadFiles` 注入 API；读取使用 `max+1` 限制，不会无界 `io.ReadAll`。multipart 临时文件在成功和失败路径均 `RemoveAll`。
- 上传响应保留扁平 `data`，并增加 `files[]` 逐文件 `succeeded/failed` 台账；`partial_failure=true` 时调用方只重传失败文件。
- 产品型数据集会在写入请求阶段按 front-matter 拆为多条产品文档；worker 对单条产品正文再切 chunk。
- 产品以数据集内型号键查重，用正文 + 元数据指纹判断变化，返回 `created`、`updated`、`unchanged`。
- 请求返回不等待 embedding；切块统计由数据库分块行实时聚合，不能把初次响应中的数量当成最终索引完成量。

仍需注意：P12 是受限读取，不是把任意大文件改成无限流式导入；多文件/多产品处理不是整个请求的一次原子事务。普通文档能否在重传时识别为同一来源，仍以现有 source/业务规则为准，不能泛化声称任意重传都幂等。

源码依据：[知识库用例](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/service.go)、[产品拆分](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/split.go)、[文件上传](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/dataset/upload_document_logic.go)、[接口契约](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/docs/dataset/dataset.api)。

### 3.4 异步索引

当前主路径：

```text
HTTP 创建/更新/上传/重建
  → 保存文档正文与元数据
  → 投递 Redis/asynq 索引任务
  → 独立 worker 读取 documents.content
  → Markdown 切块与分块持久化
  → 分批 embedding
  → 写入 Milvus；配置 ES 时写关键词索引
  → 推进文档状态为 ready / failed
```

- HTTP 进程仅投递，不消费；worker 需要独立部署。
- 队列投递失败会向调用方报错，不应误认为请求成功就会自动完成索引。
- worker 已校验必需依赖并初始化向量集合；ES 是可选依赖，但配置后不能忽略写入失败。
- Indexer 有内容指纹与分块状态收敛处理，避免每次重试无条件重做全部 embedding。
- 仍保留旧文件正文的一次性导入兼容路径，但新写入以数据库为准。

源码依据：[worker 入口](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/worker/main.go)、[Indexer](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/indexer.go)、[任务契约](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/queue/tasks/tasks.go)。

### 3.5 混合召回与知识问答工具

- 三通道召回：ES 精确匹配、ES BM25/PG 子串回落、Milvus 向量检索。
- 已支持通道权重、候选上限、RRF 融合、质量过滤及通道降级信息。
- 查询结果支持按数据集类型选择文档/分块归并粒度；文档粒度会扩展 chunk 预算并回填正文，正文过长可截断并标记。
- PostgreSQL 回落已覆盖分块正文、章节路径、文档 source 和 JSON 元数据文本，改善型号只在元数据中时的漏召。
- 文档停用状态参与结果过滤；检索范围可限定数据集。
- HTTP 的数据集搜索和对话 `search_knowledge` 使用同一套检索装配；对话工具的可查询数据集来自服务端维护的主体绑定，而非模型自选参数。
- 无 ES 时精确通道仍不可用，PG 回落也不等价于 BM25。

源码依据：[检索装配](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/restapi/retrieval.go)、[混合检索器](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/retriever.go)、[PG 回落](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/store/postgres/chunk.go)、[搜索用例](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/search.go)、[知识库工具](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/tool/knowledge.go)。

### 3.6 管理接口与代码生成

- 账号管理已有管理员建号、用户列表、昵称、用户名/昵称关键字查询。
- 主体矩阵支持主体列表、按数据集反查主体、按主体查询绑定数据集、增加和移除绑定。
- `.api` 文件按功能组拆分，统一入口导入；handler、路由、类型及 Swagger 已有生成链路。
- Makefile 提供 Ent、Go API、Swagger 和 TypeScript 生成目标；这属于契约与代码生成能力，不代表独立前端已完成验收。
- P19 用户同步本地契约已实现并有独立验收入口；真实上游条件未确认，不能计入真实外部联调完成。

源码依据：[主体列表](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/admin/list_agent_subjects_logic.go)、[数据集主体反查](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/admin/list_dataset_agents_logic.go)、[用户列表](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/user/list_users_logic.go)、[用户同步逻辑](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/user/sync_users_logic.go)、[生成目标](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/Makefile)。

## 4. API 交付清单

当前 `.api` 契约共声明 **37 条路由**，其中包含用户同步；下表按功能合并展示。除健康接口外，路径前缀为 `/api/v1`。

| 功能 | 已声明并有实现的主要接口 | 补充说明 |
| --- | --- | --- |
| 健康状态 | `GET /health`、`GET /ready` | ready 当前仅检查主数据库，不覆盖全部检索与队列依赖 |
| 身份与账号 | `POST /auth/anonymous`、`POST /auth/login`、`POST /auth/password`、`GET /auth/me` | 是否开放取决于部署配置 |
| 用户管理 | `GET /users`、`POST /users` | 用户列表当前不分页 |
| 用户同步 | `POST /users/sync` | 支持 dry-run、external_id 幂等同步；未启用 source 时返回 503，真实上游待条件确认 |
| 会话 | `POST /sessions`、`GET /sessions`、`GET /sessions/:id/messages` | 会话创建和历史 |
| 流式对话 | `POST /chat` | SSE |
| 审批 | `GET /approvals`、`GET /approvals/:id`、`POST /approvals/:id/decision`、`POST /approvals/:id/resume` | 恢复接口为 SSE |
| 主体/知识库绑定 | `GET /subjects`、`GET /dataset/:id/subjects`、`GET /agents/:subject/dataset`、`PUT/DELETE /agents/:subject/dataset/:id` | 列表与矩阵两侧查询 |
| 数据集 | `POST/GET /dataset`、`GET/DELETE /dataset/:id` | 未声明数据集更新接口 |
| 文档 | `POST/GET /dataset/:id/documents`、`GET/PUT/DELETE /dataset/:id/documents/:docId` | 创建可能返回多个产品文档 |
| 文档上传 | `POST /dataset/:id/documents/upload` | multipart，支持多个 `file` |
| 正文与启停 | `GET /dataset/:id/documents/:docId/content`、`PATCH /dataset/:id/documents/:docId/enabled` | enabled 当前实现为翻转状态，不是显式设置目标值 |
| 索引重建 | `POST /dataset/:id/documents/:docId/reindex`、`POST /dataset/:id/reindex` | 异步索引 |
| 召回 | `POST /dataset/:id/search` | 混合检索，含降级信息 |

契约依据：[REST API 总入口](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/docs/restapi.api)及其导入的功能组契约。

## 5. 当前工作区中的新增进展

以下已出现在本次读取的工作区中，但部分仍是未提交/未跟踪文件，不能只看 HEAD 提交判断进度：

1. **对话知识库检索接线补齐**：HTTP 装配注入检索用例与动态数据集绑定，知识工具补充相应测试。
2. **审批列表补齐**：新增列表 handler/logic，持久化层支持列表查询，契约与路由增加列表入口。
3. **主体矩阵补齐**：新增主体列表和数据集主体反查实现，生成类型、路由与 Swagger 同步更新。
4. **检索边界修正**：文档启停过滤、PG 元数据子串匹配及对应测试已在工作区源码中。
5. **文档同步更新**：README、架构、已知缺口、RAG 验证说明都有工作区修改，但仍有历史描述未完全收口。

本次整理只新增本文，不覆盖上述已有修改，也不提交或清理工作区。

## 6. 尚未完成与建议优先级

以下优先级是根据当前缺口给出的推进建议，不等同于承诺的交付日期。

### P0：先确保可验证、可交付

| 待办 | 当前证据/影响 | 建议完成标准 |
| --- | --- | --- |
| 修复指标配置与测试约定不一致 | 已修复：配置显式启用独立 Prometheus agent，启用/禁用测试和抓取测试已补齐 | 真实服务启动后再执行 P01-live；代码回归已通过 |
| 把召回评测接入 CI/发布 | 工具与阈值已有，仓库未发现持续执行它的流水线配置 | 每次关键变更跑评测，以阈值阻断发布并归档报告 |
| 建立真实依赖基线 | 本次未执行真实 PostgreSQL、Redis、Milvus、ES、embedding 与模型联调 | 跑通上传→排队→ready→召回→对话引用；记录失败恢复结果 |
| 生产数据库迁移 | 已改为显式 `cmd/db-migrate`，应用使用 `OpenWithoutMigration`；真实空库/历史副本/恢复仍待验收 | 在隔离数据库完成版本升级、重跑和恢复演练，避免启动期破坏性结构变更 |

### P1：补齐关键业务能力

| 待办 | 当前状态 | 建议完成标准 |
| --- | --- | --- |
| Rerank | 已有 HTTP 适配器、入口注入、候选上限、超时/降级和本地报告；真实 provider/质量仍待验收 | 在隔离环境接入 provider，比较排序前后质量和成本/P95 |
| LLM 产品拆分 | 当前仍以结构化 front-matter 为主；P16/P17 尚未确认模型、费用和标注样例 | 先确认模型与标注，再实现无头信息拆分和可重放幂等 |
| 无 ES 的精确通道 | 已补结构化相等匹配、映射复用、过滤/融合和本地报告 | 真实 API/PG/规模性能复测 |
| 上传与批量导入完善 | P12/P13 已补多文件、大小/数量边界、部分失败台账、目录导入、dry-run 和重试 | 在真实 PostgreSQL/Redis/worker/embedding 环境复测 |
| Skill 默认接入 | 已完成指定 Agent 的 list/load 接线和只读边界；真实模型工具记录待验收 | 在隔离模型环境核对实际工具调用顺序 |
| 外部用户同步 | provider-neutral source、dry-run、external_id 幂等和 no-delete 已完成本地契约 | 用户确认真实上游 endpoint、稳定 ID、凭据和密码交付后做真实验收 |
| 文档启停接口完善 | 已改为显式 enabled、父子路径校验、幂等重试和搜索过滤 | 真实 API/PG/worker 联调 |

### P2：运行与质量闭环

- 维护 Worker 已有独立入口和 dry-run/计数报告；真实数据库保留期和周期调度仍待隔离环境。
- 队列积压、失败/重试、检索各通道命中、降级和耗时业务指标已接线；真实 Prometheus 抓取和故障告警仍待验收。
- embedding 模型/维度变化的影子索引、版本切换与回滚控制平面已交付；真实 ES/Milvus 重建和多实例一致性仍待演练。
- HTTP→Redis→worker trace envelope 和 collector 配置已交付；真实 collector UI、重试及不可达演练仍待隔离环境。
- 补私有语料和拒答/无答案场景；检索单测通过不等于生成式回答不会编造。
- 统一开发说明，修正 `cmd/ragserver` 调试路径的解析器类型缺失/回落问题。

源码依据：[Rerank 接缝](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/retriever.go)、[产品解析器](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/parser/product.go)、[维护 Worker](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/maintenance/worker.go)、[迁移入口](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/storage/entx/database.go)、[文档启停实现](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/dataset/enable_document_logic.go)、[调试解析器](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/parser/parser.go)。

## 7. 本次测试与验证结果

### 7.1 已执行

- 本地工具链：Go `1.27.0`，darwin/arm64；与当前 go.mod 的 Go 声明一致。
- 全量回归：`go test -count=1 ./...`；第一次在沙箱内因临时回环端口限制失败，随后在获准的受控环境重跑通过。
- API 契约：`bash scripts/check-api.sh` 通过。
- 第一批本地入口：P01、P03、P04、P05、P06、P07、P08、P09、P12 均已有 runner；P05-P09 报告位于 `logs/acceptance/<编号>/`，每次执行会生成新的本地报告目录。

| 项目 | 结果 |
| --- | --- |
| 全量 Go 测试 | 通过，退出码 0（受控环境重跑） |
| API 生成物/手写代码检查 | 通过 |
| P05 产品拆分与增量更新 | 本地契约通过 |
| P06 检索通道/归并/降级 | 本地契约通过 |
| P07 SSE 协议 | 本地协议通过；真实模型未测 |
| P08 审批状态机 | 本地状态机通过；真实 HTTP/Agent 未测 |
| P09 动态绑定 | 本地即时生效通过；真实主体矩阵未测 |

### 7.2 已确认并处理的环境/代码问题

1. 旧的 Prometheus 配置问题已于 2026-10-07 修复：REST 配置显式启用独立 agent，测试初始化使用随机临时指标端口，并补充启用/禁用行为。
2. 沙箱默认禁止部分临时回环端口监听；这会让 `httptest`、REST 链路和 ES HTTP mock 在沙箱内误报 `operation not permitted`。本次全量回归已在获准的受控环境重跑通过，因此不计为项目失败。
3. Sonic 对当前 Go 版本不适配并回落到 `encoding/json` 的提示，以及 go-m1cpu 的 C 编译警告，未造成测试失败，继续作为工具链兼容性事项跟踪。

源码依据：[随仓库提供的 REST 配置](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/etc/restapi.yaml)、[链路与指标测试](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/chain_test.go)。

### 7.3 尚未执行

- 未启动 HTTP 服务、索引 worker 或任何真实模型调用。
- 未连接实际 PostgreSQL、Redis、Milvus、Elasticsearch 完成摄取/检索联调。
- 未运行 `cmd/rag-test` 的真实召回评测，不能给出当前 Recall、MRR 或 P95 实测成绩。
- 未检验独立前端页面、浏览器交互、部署环境、多实例一致性与故障恢复。

当前评测基础：17 条 JSONL 金标用例；阈值为通过率 ≥ 0.80、Recall@K ≥ 0.70、MRR ≥ 0.60、ACL 泄漏数 = 0、P95 ≤ 5000ms。这些是配置门槛，**不是本次测出的成绩**。

## 8. 文档与源码不一致之处

**2026-10-07 更新：** P01-P09、P12、P20 已补充本地契约验收入口；本表继续保留真实依赖、Rerank、前端与生产化能力的未完成边界，不能把本地回归直接计为真实联调完成。

| 旧描述 | 当前源码事实 | 本文采用的口径 |
| --- | --- | --- |
| 已知缺口仍写“接口不接收 multipart” | 上传逻辑调用 `ParseMultipartForm` 并读取多个 `file` | 上传已实现，流式化和批量目录导入仍待补 |
| 部分说明仍写正文落到托管目录 | 知识库主路径保存/读取 `documents.content` | 数据库是正文真相，旧目录仅作兼容导入 |
| 部分说明把产品拆分与 chunk 切分都写在 worker | 产品数据集写入时拆产品，worker 对单条正文切 chunk | 区分请求期产品拆分与后台 chunk 切分 |
| 总契约注释称返回时 chunk_count 已是最终值 | 实际切块发生在后台，统计聚合分块行 | 初次响应数量不是最终索引完成量 |
| README 称默认 HTTP 端口为 8080 | 随仓库提供的 REST 配置为 8090 | 实际使用以运行时传入的 REST 配置为准 |
| 配置存在 `Retrieval.EnableRerank` | 无实现或入口注入 | 不计为已完成重排 |
| 说明中提到前端页面已完成 | 本次仅审阅当前后端仓库 | 不把前端完成度计入本次验证范围 |

上述差异说明：后续评估进度应优先核对业务实现、入口接线与测试结果，而不能只依据历史 README 或 TODO 描述。

## 9. 建议下一阶段里程碑

### 里程碑 A：可复现的联调基线

- [ ] 解决 Prometheus 配置/测试约定冲突，全量测试全绿。
- [ ] 明确并启动 HTTP + worker 及全部真实依赖。
- [ ] 验证上传、状态轮询、召回、知识问答引用的主流程。
- [ ] 记录真实召回评测基线并提交报告。
- [ ] 整理当前未提交源码，确认变更范围后再提交。

### 里程碑 B：关键能力闭环

- [ ] Rerank 具体实现、候选预算、降级策略与效果测试。
- [ ] 明确 LLM 产品拆分方案并完成契约测试。
- [ ] 补私有语料评测、无答案评测、目录导入与上传边界测试。
- [ ] 完成默认 Skill 使用方案和用户同步数据源方案。

### 里程碑 C：生产交付

- [ ] 版本化数据库迁移与发布流程。
- [ ] CI 质量门禁、跨版本评测归档。
- [ ] 维护清理接线、业务指标、告警与全链路追踪验证。
- [ ] 索引版本切换、回滚及异常恢复演练。

---

**一句话进度：第一批 P01-P09、P12、P20、P21、P22、P23 的本地代码与契约验收已经形成可复跑闭环；当前剩余关键工作是用隔离 PostgreSQL/Redis/worker/模型/Milvus/ES 复测真实链路，并完成 P20 真实召回基线。**


## 10. P02 配置与预检交付补充（2026-10-07）

本节补充上述源码快照之后的配置改动，不将其等同于真实服务验收。

- API 业务配置改为 `configs/api/config.yaml`（`EINO_API_CONFIG`）；HTTP 配置改为 `configs/api/restapi.yaml`（`EINO_REST_CONFIG`）。
- worker 配置改为 `configs/worker/config.yaml`（`EINO_WORKER_CONFIG`），不再要求 API 模型、身份、HTTP 或工作区配置，也不创建工作区；两个入口不再读取 `EINO_CONFIG`。
- API 使用纯队列投递客户端；消费并发、队列权重、消费重试间隔和退出超时只放在 worker 配置。
- 已交付 `configs/acceptance/api.yaml`、`worker.yaml` 和 `restapi.yaml`，采用独立验收库 / Redis DB / collection，分别落盘 API 和 worker 日志。
- 已提供两个入口的 `-check-config`；配置及队列回归测试、入口相关包测试、两个离线配置检查均通过。
- P01 指标配置与全量回归已于 2026-10-07 完成并通过；P02 的预检、报告、fixture 和显式清理入口已交付，但真实依赖通过仍需用户环境复测。

详细修改、迁移和用户直接验证步骤见 [P02 独立配置指南](p02-configuration.md)；逐项计划见 [用户验收计划](user-acceptance-plan.md)。


## 11. P03 / P04 交付补充（2026-10-06）

此节覆盖上述历史快照后的增量，不修改原测试结果。

- P03 已纠正文档中的 source/正文存储、multipart、产品拆分和异步计数说明。
- 请求的真正选填字段统一为 go-zero `optional`；`.api`、Go types、Swagger 已同步。
  Swagger 增加声明式扩展，表达 multipart file、错误响应和响应 omitempty；routes 未变化，
  用户此前已补齐的主体/分页类型保留。
- 新增 `make check-api`：在临时镜像中生成、严格比较，再用 overlay 编译；不删手写实现。
- P04 增加真实 Service/Indexer/Ent 内存库/Markdown Pipeline 生命周期回归和实际 HTTP
  handler 契约测试；覆盖创建、更新、读取、搜索、单篇/整库重建、删除、故障恢复和任务幂等。
- 修复分块事务等非 embedding 阶段在重试耗尽后不落 failed 的问题；正文读取统一错误映射。
  multipart 解析产生的临时文件在请求结束时清理，未实现大文件流式读取。
- 已新增编号执行器 `scripts/acceptance/run.sh`：P02、P03、P04 本地/真实模式、暂停消费和投递故障模式；并补充 P05-P09、P12、P20、P21、P22、P23 的本地契约入口。P02 输出脱敏预检报告和唯一 fixture，P03/P04 提供有界等待、HTTP 报告及本轮资源台账/清理。
- 本地回归和生成检查已通过；真实端到端及 PostgreSQL/Milvus/ES/模型实际条目核对未执行。
  P01-P09、P12、P20、P21、P22、P23 的本地结果不等同于真实外部依赖端到端验收；P02 的真实服务通过结果仍待用户环境复测。

用户直接操作与准确的验证边界见 [P03/P04 验收指南](p03-p04-acceptance.md)。


## 12. P12 上传限制与部分失败交付补充（2026-10-07）

P12 已完成代码和本地契约验收，入口为 `bash scripts/acceptance/run.sh P12`。本地测试直接调用真实上传 handler，覆盖：

- 两个 `file` 字段正常上传，并保持 `data` 兼容字段；
- 空文件、`.pdf` 等不支持格式、单文件超限、请求总大小超限、文件数量超限；
- 一个成功文件和一个失败文件同时提交，响应中的 `files[]` 明确给出失败文件的 `code/message`；
- 成功文件继续入库，失败文件不把整批伪装成成功；
- `ParseMultipartForm` 产生的临时文件在请求结束时清理。

API 配置中的默认边界是：单文件 5 MiB、multipart 请求总量 50 MiB、每批最多 10 个文件；实际值以 `configs/api/config.yaml` 或 `configs/acceptance/api.yaml` 为准。P12 本地通过不等于 PostgreSQL/Redis/worker/embedding/Milvus/ES 的真实联调已通过，后者仍按 P04/P05/P06 的环境验收执行。


## 13. 第一批 P05-P09 本地契约补充（2026-10-07）

本轮按照第一批顺序补齐了 P05、P06、P07、P08、P09、P20 的可直接执行入口。这里的“完成”指本地确定性契约/状态机回归通过，不把受控桩、Ent SQLite 或协议测试冒充真实外部依赖端到端。

| 项目 | 本地已覆盖 | 用户直接验证 | 当前边界 |
|---|---|---|---|
| P05 | 产品拆分、稳定 `external_key`/ID、原样重传 `unchanged`、单产品正文/元数据更新、失败后重试、坏 YAML/重复型号拒绝 | `bash scripts/acceptance/run.sh P05` | 真实 PostgreSQL/Redis/worker/embedding 待复测 |
| P06 | exact/keyword/vector 成功与降级、归并、粒度、预算、截断元数据 | `bash scripts/acceptance/run.sh P06` | 真实 PostgreSQL/Milvus/ES 和固定语料待复测 |
| P07 | HTTP 路由、请求校验、生成格式 SSE 帧 | `bash scripts/acceptance/run.sh P07` | 真实模型、历史、引用、断流待复测 |
| P08 | 审批状态、检查点、批准、Claim/Consume 单次保护、审计脱敏 | `bash scripts/acceptance/run.sh P08` | 真实 HTTP/Agent/workspace 与重启恢复待复测 |
| P09 | 每次知识工具调用重新解析主体绑定，绑定变更即时生效 | `bash scripts/acceptance/run.sh P09` | 真实主体矩阵 API 与双身份对话待复测 |
| P20 | JSONL 金标校验、阈值门禁、指标计算、失败记录和逐用例报告渲染 | `bash scripts/acceptance/run.sh P20` | 真实 PostgreSQL/Milvus/ES/embedding 召回基线和 P95 待复测 |

统一回归命令：

```bash
bash scripts/acceptance/run.sh --list
bash scripts/acceptance/run.sh P05
bash scripts/acceptance/run.sh P06
bash scripts/acceptance/run.sh P07
bash scripts/acceptance/run.sh P08
bash scripts/acceptance/run.sh P09
bash scripts/acceptance/run.sh P20
go test -count=1 ./...
bash scripts/check-api.sh
```

本轮报告目录示例：P05 为 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P05/20261007-103825-43717-local`；P06/P07/P08/P09/P20 的报告目录由脚本每次执行打印并写入 `logs/acceptance/<编号>/`。P20 本地报告不包含真实 Recall/MRR/P95；真实联调前仍需先执行 P02 预检，并保持 API 与 worker 配置隔离。


## 14. 第二批 P21-P23 本地质量门禁补充（2026-10-07）

本轮开始推进第二批“质量可持续”目标，先把不依赖真实模型和外部服务的确定性门禁落地；真实主体矩阵、真实回答和 CI 平台接入仍单独标记。

| 项目 | 本地已覆盖 | 用户直接验证 | 当前边界 |
|---|---|---|---|
| P21 | 两份非空合成私有 corpus、正向/forbidden 金标、注入跨主体泄漏时失败、动态绑定刷新 | `bash scripts/acceptance/run.sh P21` | 真实 API/对话/多通道主体矩阵待复测 |
| P22 | 必需事实、禁止事实、明确无答案、引用格式和授权 source 校验 | `bash scripts/acceptance/run.sh P22` | 真实模型、冲突资料和人工问答判定待复测 |
| P23 | 全量 Go 回归、API 契约、P20-P22 本地门禁、分项日志和脱敏 `summary.json` | `bash scripts/acceptance/run.sh P23` 或 `make quality-gate` | CI 平台、真实评测依赖、报告归档和跨版本指纹待确认 |

P23 最近一次本地报告：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P23/20261007-110435-50508-local`。本地门禁通过不等于真实质量门禁已接入 CI；下一步应先确认 CI 平台和隔离依赖，再接入真实 P20/P21/P22。

## 15. P10 文档启停幂等状态交付（2026-10-07）

P10 已完成本地代码和确定性契约验收，入口为 `bash scripts/acceptance/run.sh P10`。本轮将文档启停从“每次请求翻转”改为“设置明确目标值”：

- `PATCH /api/v1/dataset/{id}/documents/{docId}/enabled` 必须携带 `{"enabled":true}` 或 `{"enabled":false}`；缺失字段会被拒绝，不会静默当成 `false`。
- 应用服务按数据集 ID + 文档 ID 联合定位，跨数据集父子路径不匹配时不会修改文档。
- 重复发送相同目标值是幂等的；停用不删除正文、分块或索引内容，搜索不再召回；重新启用后恢复召回。
- 本地回归覆盖连续 `false`、连续 `true`、缺失字段语义、跨数据集保护、正文保留和搜索可见性。

最近一次本地报告会写入 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P10/`。真实 API 进程重启后的状态持久化，以及 PostgreSQL/Redis/worker/检索服务联调仍需在隔离环境复测。

## 16. P11 无 ES 结构化精确检索交付（2026-10-07）

P11 已完成本地契约实现与验收，入口为 `bash scripts/acceptance/run.sh P11`，最近一次报告：

`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P11/20261007-120219-66316-local`

本轮完成：

- `configs/es/chunk_mapping.yaml` 继续作为唯一业务字段声明；`ExactField` 同时携带 ES 字段名、metadata 路径和多值声明，PG 不再另写 model/product_id 清单。
- exact 值统一做大小写归一、首尾空格裁剪和连续空格折叠；保留规范化整串，因此 `ACC-H105P` 不会被连字符拆成只能命中片段。
- ES 不可用时，PostgreSQL 在 metadata 上执行真正的结构化相等匹配；`ACC-H105P` 与 `ACC-H105PE` 不会因前缀关系互相冒充 exact。
- 回落路径保留 enabled、dataset、visibility、owner 过滤，支持 `variants` 多值字段，并经过既有 `reload` 再次读取 PostgreSQL 现值。
- exact 回落可进入 HybridRetriever 融合，并在通道报告中标记为 `exact`；无 mapping 时才关闭该通道，数据库错误不会静默改走 substring。
- 增加了独立的无 ES、近似型号、过滤、融合和规范化测试，以及用户手工验收文档 `docs/p11-exact-search-acceptance.md`。

当前边界：本地契约不等于真实 HTTP/外部 PostgreSQL 联调；PG 回落是正确性优先的 metadata 扫描，规模性能需在隔离依赖环境单独压测。完整 ES HTTP 测试在受限沙箱中因 `httptest` 监听权限未完成，需要在允许本地监听的环境复跑。

## 17. P13 目录批量导入与增量重跑交付（2026-10-07）

- 已新增 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/knowledge-import/` 独立 CLI，API 与 worker 配置/进程职责不混用；正式导入复用现有 `knowledge.Service` 和索引队列。
- 已新增 `internal/application/knowledge/directory_import.go`：递归扫描 Markdown/纯文本、可排除相对路径、根目录边界和软链接目标校验、单文件大小限制、空文件失败。
- 已实现稳定 `source`（dataset + 相对路径主干 + 路径哈希）、正文 SHA-256 增量判定、dry-run、逐文件报告、失败清单、`--retry-report` 只重试失败项，以及源文件消失不删库。
- 已新增 `directory_import_test.go` 覆盖首次导入、原样重跑、单文件更新、失败重试、外部软链接和根目录边界。
- 本地验收：`bash scripts/acceptance/run.sh P13` 已通过，报告：`logs/acceptance/P13/20261007-121947-70022-local/`。
- 真实 PostgreSQL/Redis/worker/embedding/Milvus 联调仍需按 `docs/p13-directory-import-acceptance.md` 在目标环境执行；未把本地契约测试冒充成真实依赖通过。

## 18. P14/P15 Rerank 适配、预算与降级交付（2026-10-07）

本轮继续推进第三批的 `P14 → P15`：

- 新增 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/rerank/http.go`，提供供应商中立的 HTTP Rerank 适配器；请求携带 model/query/documents，响应按 index + relevance_score 返回排序，非法/重复/越界结果拒绝。
- `RetrievalConfig` 新增 `Retrieval.RerankEndpoint`、`Retrieval.RerankAPIKeyEnv`、`Retrieval.RerankModel`、`Retrieval.RerankTimeoutMS`；密钥只从环境变量读取。API 组合根在 `Retrieval.EnableRerank=true` 时才注入适配器，未配置 endpoint 会在配置校验阶段明确失败。
- `HybridRetriever` 对重排候选数、超时、取消和返回子集做约束；重排故障不会丢弃已完成的融合召回，`Report.Rerank` 记录 enabled/candidates/applied/error，`Degraded()` 暴露 `rerank`。
- 本地验收已通过：
  - `bash scripts/acceptance/run.sh P14` → `logs/acceptance/P14/20261007-123739-73238-local/`
  - `bash scripts/acceptance/run.sh P15` → `logs/acceptance/P15/20261007-123743-73225-local/`
- 当前边界：P14/P15 的本地契约和 API 配置接线已完成；真实供应商、模型质量、调用成本/P95 以及故障演练仍需在隔离环境提供 endpoint 后，按 `docs/p14-rerank-acceptance.md` 与 `docs/p15-rerank-acceptance.md` 复测。

## 19. P18 默认 Agent Skill 接线交付（2026-10-07）

- `workspace_agent` 现在显式取得 `list_skills`、`load_skill`，并在指令中要求“先列出，再按需加载”；Skill 只提供指导，不改变已有的写入、执行、审批和工作区边界。
- 新增 `docs/p18-skill-acceptance.md`，说明唯一标题 Skill、工具调用顺序、Skill 缺失和越权文字的直接验证方式。
- 本地验收：`bash scripts/acceptance/run.sh P18` 已通过，报告：`logs/acceptance/P18/20261007-124020-73719-local/`。
- 当前边界：真实模型调用记录、指定 Agent 的实际 list/load 顺序，以及 Skill 移除后的对话行为，仍需在有模型密钥的隔离环境复测。

## 20. P24 维护清理 dry-run 与报告交付（2026-10-07）

- 新增 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/maintenance/` 独立入口，支持 `-dry-run`、JSON 报告和真实执行计数；不会与 API/worker 配置混用。
- `internal/maintenance/worker.go` 增加只读预览和按审批、checkpoint、turn 分类的 mutation report；终态边界和重跑幂等仍由原有清理端口负责。
- 本地验收：`bash scripts/acceptance/run.sh P24` 已通过，报告：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P24/20261007-124849-77312-local/`。
- 真实数据库保留期、备份恢复和周期调度仍需在隔离数据库验证；不得为了验收缩短生产保留期。

## 21. P25/P26 业务可观测性与跨进程 trace 交付（2026-10-07）

- `internal/platform/observability/metrics.go` 新增队列积压、索引 success/failure/retry、检索通道 hit/empty/error/degraded、检索耗时和 component health 指标；标签只使用固定状态/通道/队列名，不包含正文、用户 ID 或请求凭证。
- worker 每 10 秒通过 Asynq Inspector 采样队列深度；knowledge Service/Indexer 在真实操作路径记录检索与索引业务结果。HTTP 继续使用 go-zero 独立 Prometheus 端口，不把 `/metrics` 混进业务路由。
- `configs/observability/otel-collector.yaml` 提供受内存上限和 batch 约束的本地 OTLP collector 样例；`internal/platform/queue/asynq/trace_test.go` 验证 HTTP→publish envelope→worker process 共用一个 trace ID。
- 本地验收已通过：
  - `bash scripts/acceptance/run.sh P25 --local` → `logs/acceptance/P25/20261007-130515-81829-local/`
  - `bash scripts/acceptance/run.sh P26 --local` → `logs/acceptance/P26/20261007-130521-81826-local/`
- 当前边界：真实 Prometheus 抓取、暂停 worker/中断 ES、OTLP collector UI 展示、任务重试和 collector 不可达演练仍需隔离环境；本地通过不等于四批次全部完成。

## 22. P27/P28/P29 生产化边界与索引版本控制平面交付（2026-10-07）

- P27 新增 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/db-migrate/` 显式迁移入口和 `internal/platform/storage/entx/migrations/` 版本化 SQL；支持 `-dry-run`、`-apply`、`-status`，以 `schema_migrations` 记录幂等执行状态，并用 advisory lock 串行化显式迁移。
- API、worker、导入和维护等应用入口已改用 `entx.OpenWithoutMigration`，不会在启动时隐式执行 DDL；开发便利迁移单独命名为 `OpenDevelopment`。生产连接路径不再启用 `WithDropColumn`/`WithDropIndex`。
- P27 本地验收已通过：`bash scripts/acceptance/run.sh P27 --local`，报告：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P27/20261007-131819-83716-local/`。
- P28/P29 已新增 `internal/indexversion` 控制平面与 `cmd/index-version`：版本元数据、building/ready/failed 状态、进度、显式切换/回滚和失败门禁均可持久化并单独验证。
- P28 本地验收已通过：`bash scripts/acceptance/run.sh P28 --local`，报告：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P28/20261007-132256-84343-local/`；P29 本地验收已通过：`bash scripts/acceptance/run.sh P29 --local`，报告：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P29/20261007-132259-84341-local/`。
- 当前边界：P27 仍需真实 PostgreSQL 空库/历史副本/备份恢复；P28/P29 仍需真实 ES/Milvus 独立索引、逐版本评测、多实例切换和故障恢复演练。本轮没有把控制平面测试冒充成真实外部依赖完成。

## 23. P16/P17 LLM 产品拆分与重放边界交付（2026-10-07）

- P16 新增 `internal/rag/parser/llmsplit/` provider-neutral HTTP 适配器：请求带模型/解析版本/原文，响应必须是有稳定产品键的结构化产品数组；数量、空正文、缺键和重复键在写库前拒绝。当时计划的 `Knowledge.ProductSplitEnabled` 等 API 配置字段最终没有落到 `internal/platform/config`（拆分器只有组合根注入点）；验收模板里残留的死键已在 2026-10 的配置迁移中删除。
- 产品数据集在无 front-matter 时才调用可选拆分器；结构化 front-matter 路径保持原样。LLM 失败不会静默退回整份成功文档。转换后复用现有产品文档 upsert、索引队列和可见性规则。
- P17 将输入摘要、模型、解析版本纳入重放指纹，并把解析版本/输入摘要写入 metadata；`external_key` 唯一约束与冲突重查继续保护并发同型号上传，版本变化触发明确 update。
- 本地验收已通过：P16 报告 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P16/20261007-134920-91122-local`；P17 报告 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P17/20261007-135003-91121-local`。
- 当前边界：真实模型质量、费用/时延、人工金标，以及 PostgreSQL/Redis/API/worker 并发、重启、任务重试和显式重解析仍待隔离环境；本轮没有把本地模型契约冒充成真实 LLM 质量完成。


## 24. P19 外部用户同步本地契约交付（2026-10-07）

- P19 已去掉原先恒定 501 的占位：新增 provider-neutral HTTP 用户源、`external_id` 稳定匹配键、API-only `Auth.UserSync` 配置，以及管理员同步请求的 `dryRun` 与结果台账。
- 同步只新增/更新/跳过，不因上游缺人删除本地账号；同名手工账号不会被自动认领。新账号随机生成一次性初始密码，保持 `must_change_password=true`，上游不能覆盖本地角色、状态或密码。
- 用户表新增可空唯一 `external_id`，配套显式迁移 `0003_user_external_id`；worker 不读取用户同步配置。
- 本地验收入口：`bash scripts/acceptance/run.sh P19 --local`。真实上游仍待 source endpoint、协议/分页、稳定 ID、验收凭据和密码下发方案确认，本轮没有把 fake source 冒充真实上游完成。
## 25. 四批次本地验收汇总（2026-10-07）

- 第一批已逐项复跑：P01、P03、P04、P05、P06、P07、P08、P09、P12、P20；P01 全量 Go 回归通过，P03 API 生成物检查通过，其余编号的本地验收报告均已生成。
- 第二批已逐项复跑：P21、P22、P23、P27；质量门禁、私有语料隔离、回答证据、显式迁移边界均通过本地契约验收。
- 第三批已逐项复跑：P10、P11、P13、P14、P15、P16、P17、P18、P19；文档启停、无 ES 精确检索、目录导入、Rerank、LLM 拆分/重放、Skill 和用户同步本地契约均通过。
- 第四批已逐项复跑：P24、P25、P26、P28、P29；维护清理、业务指标、跨进程 trace、影子索引和切换回滚控制平面均通过本地契约验收。
- P02 已验证配置、健康接口和报告脱敏；本地 acceptance compose 不是前置条件。真实预检应改用现有部署配置（`configs/api/config.yaml`、`configs/worker/config.yaml`、`configs/api/restapi.yaml`）并执行 `bash scripts/acceptance/run.sh P02 --existing --degraded`。
- 本轮总门禁：`GOCACHE=/tmp/eino-go-cache go test ./... -count=1`、`bash scripts/check-api.sh`、`git diff --check` 均通过。

报告根目录：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/`。这些是本地确定性/契约验收；真实 PostgreSQL、Redis、worker、模型、Milvus、ES、Prometheus/OTLP 和多实例故障演练仍需隔离环境。
## 26. P24 周期维护入口补齐（2026-10-07）

- worker 配置现在显式包含 `Maintenance` 段；`LoadWorker` 会校验周期、保留期和批大小，避免维护命令以零值运行并误删数据。
- `cmd/maintenance` 新增 `-periodic`，启动后立即清理一次，再按配置周期执行；每次成功运行通过 `OnReport` 覆盖/输出最新 JSON 报告。
- 维护 Worker 增加无效周期/批大小保护、空 logger 兜底和取消信号优雅退出测试。
- 本地验证：`GOCACHE=/tmp/eino-go-cache go test ./internal/maintenance ./internal/platform/config -count=1` 已通过；真实数据库清理仍需在隔离数据库执行。

## 27. P01 最终复验记录（2026-10-07）

- 已重新执行 `bash scripts/acceptance/run.sh P01`，全量 Go 回归、Prometheus 启用/禁用子进程测试、HTTP middleware 到独立指标端口抓取测试均通过。
- 本次原始日志：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P01/20261007-150111-9117/full-go-test.log`。
- 本次验收报告目录：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P01/20261007-150111-9117`。
- 用户可直接复验：`cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2 && bash scripts/acceptance/run.sh P01`。


## 28. P25 业务指标接入 Prometheus agent（2026-10-07）

- API 与 worker 创建业务指标后，现通过 `RegisterDefault()` 注册到 go-zero Prometheus agent 使用的默认 registry；`eino_harness_queue_depth`、`eino_harness_index_tasks_total`、`eino_harness_retrievals_total` 等指标可与 Go-Zero HTTP 指标一起被抓取。
- 新增 `TestBusinessMetricsCanBeRegisteredForPrometheusAgent`，验证注册到独立 Prometheus registry 后仍能抓取指标，避免只验证私有测试 registry。
- 本地验证：`bash scripts/acceptance/run.sh P25 --local` 已通过；报告：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P25/20261007-151620-12893-local`。
- `component_health` 状态切换会把上一个状态置为 0，保证同一组件始终只有一个 active 状态。
- 真实 Prometheus 抓取、队列积压变化和 ES 故障演练仍需 P02 隔离依赖环境。


## 29. P02 本地依赖启动脚本补齐（2026-10-07）

- 新增可选的 `configs/acceptance/compose.yaml`，只为本地验收启动 PostgreSQL、Redis、Milvus 及 etcd / MinIO 支撑服务；容器端口仅绑定 `127.0.0.1`，API、worker、embedding、ES 和 OTLP collector 仍需单独启动或提供。
- 新增 `scripts/acceptance/dependencies.sh`，提供 `up`、`status`、`logs`、`down` 四个受控操作；`scripts/acceptance/run.sh P02-deps ...` 可作为统一入口，仅使用 compose 项目 `eino-acceptance`，`down` 默认保留 named volume，不执行全库清理。
- 新增 `configs/acceptance/.env.example`，密码只通过 `EINO_STORAGE_PASSWORD` 环境变量提供，不把本地凭据写入配置。
- 已验证：`bash -n scripts/acceptance/dependencies.sh`、`bash scripts/check-api.sh`、`git diff --check`。compose 模板只保留为可选隔离环境，本项目默认不要求本地 Docker。
- 本地 Docker 依赖栈为可选项，本轮不启动容器；已有数据库和服务时直接使用现有配置执行 `bash scripts/acceptance/run.sh P02 --existing --degraded`，并将 API/worker 的真实运行证据归档到对应报告。


## 30. 四个交付批次聚合验收完成（2026-10-07）

- 新增 `scripts/acceptance/run-batch.sh`，并接入 `scripts/acceptance/run.sh batch <1|2|3|4> --local`；每个批次输出 `summary.json` 和各计划原始日志。
- 第一批本地报告：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/batch-1/20261007-154134-18619-local`，状态 passed。
- 第二批本地报告：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/batch-2/20261007-154427-21682-local`，状态 passed。
- 第三批本地报告：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/batch-3/20261007-154646-22850-local`，状态 passed。
- 第四批本地报告：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/batch-4/20261007-154956-23761-local`，状态 passed。
- 四批次聚合只证明本地确定性契约、配置和状态机；真实依赖、真实模型、CI 平台、多实例和故障演练仍按 `docs/delivery-batches.md` 的真实出口执行。

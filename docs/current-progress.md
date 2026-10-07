# Eino Harness 当前开发进度

> 整理日期：2026-10-06（Asia/Shanghai）  
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

**不能认定已经全部完成：** Rerank、LLM 产品拆分、外部用户同步、持续评测门禁、维护任务接线、生产数据库迁移等仍有明确缺口；本次全量 Go 测试也尚未全绿。

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
| 账号与用户管理 | 部分完成 | 匿名身份、登录、首次改密、管理员建号、昵称和关键字查询 | 用户同步占位；管理生命周期不完整 |
| 数据集管理 | 已实现 | 创建、列表、详情、删除、主体绑定及绑定反查 | 没有完整的数据集编辑/状态切换接口 |
| 文档管理与上传 | 已实现，仍需完善 | CRUD、正文读取、启停、单文档/整库重建、multipart 多文件上传 | 非全链路流式上传；目录批量导入待补 |
| 产品文档拆分 | 部分完成 | front-matter 解析、一产品一文档、型号查重、正文与元数据指纹 | 未实现 LLM 自动识别产品边界 |
| 异步索引 | 已实现 / 待验证 | Redis/asynq、独立 worker、分批 embedding、状态推进与重试收敛 | 真实故障注入和恢复验收待补 |
| 混合召回 | 已实现 | 精确 + BM25/PG 回落 + 向量；加权 RRF、通道降级信息 | 无 ES 时精确通道缺席；Rerank 未接入 |
| 对话知识库工具 | 已实现 | 按服务端主体绑定查询数据集，跨库结果合并、来源引用 | 无授权/依赖故障有明确返回；实际回答质量待测 |
| 检索质量评测 | 部分完成 | 17 条金标用例、指标计算、阈值配置和退出码门禁 | 未接 CI、缺跨版本留档与完整私有语料评测 |
| 运行可观测性 | 部分完成 | 结构化日志、追踪接线、健康接口、go-zero 指标接线 | 默认 Prometheus 配置被注释；本次测试失败 |
| 数据保留与清理 | 部分完成 | 清理过期审批、检查点和终态轮次的 Worker 已编写 | 未被入口进程启动 |
| 生产化 | 部分完成 | 配置校验、worker 依赖校验、退出处理等已存在 | 版本化迁移、索引版本切换、发布门禁与告警待补 |

## 3. 已完成能力详解

### 3.1 多 Agent 与工具体系

- `root_agent` 负责请求分派，不直接承担专项工具操作。
- `knowledge_agent` 使用 `search_knowledge` 获取知识库内容并保留引用。
- `workspace_agent` 被授予 `read_file`、`list_dir`。
- `automation_agent` 被授予 `write_file`，执行模式非 disabled 时增加 `shell`。
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

- 数据集已有创建、列表、详情与删除；文档已有创建、读取、更新、删除、正文查询、启停、重建索引。
- 文档正文以 PostgreSQL 的 `documents.content` 为准，`source` 是逻辑溯源信息，不应当作为当前正文文件路径使用。
- multipart 上传**已经实现**，文件字段为 `file`，支持同一请求携带多个文件，并调用与 JSON 创建相同的知识库用例。
- 产品型数据集会在写入请求阶段按 front-matter 拆为多条产品文档；worker 对单条产品正文再切 chunk。
- 产品以数据集内型号键查重，用正文 + 元数据指纹判断变化，返回 `created`、`updated`、`unchanged`。
- 请求返回不等待 embedding；切块统计由数据库分块行实时聚合，不能把初次响应中的数量当成最终索引完成量。

仍需注意：multipart 解析有内存阈值，但单文件随后仍使用 `io.ReadAll`，因此不是端到端流式大文件摄取；多文件/多产品处理也不是整个请求的一次原子事务。

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
- 用户同步当前明确返回 501，不能计入已完成业务功能。

源码依据：[主体列表](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/admin/list_agent_subjects_logic.go)、[数据集主体反查](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/admin/list_dataset_agents_logic.go)、[用户列表](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/user/list_users_logic.go)、[用户同步占位](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/user/sync_users_logic.go)、[生成目标](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/Makefile)。

## 4. API 交付清单

当前 `.api` 契约共声明 **37 条路由**，其中包含用户同步占位；下表按功能合并展示。除健康接口外，路径前缀为 `/api/v1`。

| 功能 | 已声明并有实现的主要接口 | 补充说明 |
| --- | --- | --- |
| 健康状态 | `GET /health`、`GET /ready` | ready 当前仅检查主数据库，不覆盖全部检索与队列依赖 |
| 身份与账号 | `POST /auth/anonymous`、`POST /auth/login`、`POST /auth/password`、`GET /auth/me` | 是否开放取决于部署配置 |
| 用户管理 | `GET /users`、`POST /users` | 用户列表当前不分页 |
| 用户同步 | `POST /users/sync` | **占位，返回 501** |
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
| 修复指标配置与测试约定不一致 | Prometheus 配置被注释，当前 2 个 REST API 测试失败 | 明确默认启用还是关闭指标，调整配置/测试契约；全量测试转绿并验证抓取 |
| 把召回评测接入 CI/发布 | 工具与阈值已有，仓库未发现持续执行它的流水线配置 | 每次关键变更跑评测，以阈值阻断发布并归档报告 |
| 建立真实依赖基线 | 本次未执行真实 PostgreSQL、Redis、Milvus、ES、embedding 与模型联调 | 跑通上传→排队→ready→召回→对话引用；记录失败恢复结果 |
| 生产数据库迁移 | 启动仍直接 `Schema.Create`，启用删列/删索引选项 | 采用版本化迁移和上线前审核，避免启动期破坏性结构变更 |

### P1：补齐关键业务能力

| 待办 | 当前状态 | 建议完成标准 |
| --- | --- | --- |
| Rerank | 有接口和融合后的调用点，无具体实现；入口未注入，候选上限未消费 | 实现并注入；限制候选数；补超时/降级与排序效果测试 |
| LLM 产品拆分 | 当前仅按 front-matter 拆分 | 接入自动识别产品边界，保持正文/元数据契约及稳定指纹 |
| 无 ES 的精确通道 | 当前标记降级，不执行结构化精确匹配 | 补型号等字段的相等匹配，区分 exact 与 substring |
| 上传与批量导入完善 | multipart 已有，但文件完整读取；未见目录导入入口 | 明确文件格式/大小边界、失败语义，补目录批量导入和上传测试 |
| Skill 默认接入 | 注册存在，专项 Agent 工具集合未包含 | 明确可用 Agent，补工具授予与路由验证 |
| 外部用户同步 | 当前恒返回 501 | 明确数据源、匹配键和结果契约后实现真实同步 |
| 文档启停接口完善 | 当前翻转状态；实现仅按 docId 取记录，未使用路径 datasetId 校验归属 | 校验父子路径一致性，考虑显式目标状态与重试语义，补边界测试 |

### P2：运行与质量闭环

- 将维护 Worker 接入独立入口或周期调度，真正执行过期审批、检查点和轮次清理。
- 增加索引队列积压、失败/重试、检索各通道命中、降级和耗时的业务指标及告警。
- 设计 embedding 模型/维度变化时的影子索引、版本切换与回滚，避免原地重建混用版本。
- 补跨 HTTP→Redis→worker 的实际追踪验证，统一多个配置源中的服务命名。
- 补私有语料和拒答/无答案场景；检索单测通过不等于生成式回答不会编造。
- 统一开发说明，修正 `cmd/ragserver` 调试路径的解析器类型缺失/回落问题。

源码依据：[Rerank 接缝](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/retriever.go)、[产品解析器](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/parser/product.go)、[维护 Worker](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/maintenance/worker.go)、[迁移入口](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/storage/entx/database.go)、[文档启停实现](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/dataset/enable_document_logic.go)、[调试解析器](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/parser/parser.go)。

## 7. 本次测试与验证结果

### 7.1 已执行

- 本地工具链：Go `1.27.0`，darwin/arm64；与当前 go.mod 的 Go 声明一致。
- 命令：`GOCACHE=/private/tmp/eino-progress-gocache go test ./...`。
- 初次在沙箱内执行，部分测试因本机临时端口监听受限失败；随后获准在沙箱外重跑。
- 以下统计以**沙箱外重跑**为准，不把首次环境限制误报为项目缺陷。

| 项目 | 结果 |
| --- | --- |
| 测试文件 | cmd/internal 下共 37 个 `*_test.go` 文件 |
| 通过的带测试包 | 18 个（包含缓存命中） |
| 失败的带测试包 | 1 个：`eino-quickstart/internal/transport/restapi` |
| 未含测试的包 | 65 个 |
| 失败测试 | 2 个，均在 REST API 指标配置/抓取测试中 |
| 全量测试退出码 | 1，**当前不是全绿状态** |

### 7.2 确认的失败点

1. `TestShippedConfigKeepsAgentsConfigurable`：`Prometheus.Host` 和 `Prometheus.Path` 为空。
2. `TestPrometheusAgentServesGoZeroMetrics`：抓取本机指标地址时连接被拒绝。

当前随仓库提供的 REST 配置将整个 `Prometheus` 段注释，配置自身的注释也说明会导致相关测试失败。测试初始化只覆盖 host/port，未补 path，因此指标抓取闭环没有建立。这里是**配置与测试预期不一致**，本次只记录，没有顺手修改业务配置。

此外，输出有 Sonic 对当前 Go 版本不适配并回落到 `encoding/json` 的提示，以及 go-m1cpu 的 C 编译警告；本次未因此产生额外失败包，应作为工具链兼容性事项跟踪，而不是当作所有测试失败的原因。

源码依据：[随仓库提供的 REST 配置](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/etc/restapi.yaml)、[链路与指标测试](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/chain_test.go)。

### 7.3 尚未执行

- 未启动 HTTP 服务、索引 worker 或任何真实模型调用。
- 未连接实际 PostgreSQL、Redis、Milvus、Elasticsearch 完成摄取/检索联调。
- 未运行 `cmd/rag-test` 的真实召回评测，不能给出当前 Recall、MRR 或 P95 实测成绩。
- 未检验独立前端页面、浏览器交互、部署环境、多实例一致性与故障恢复。

当前评测基础：17 条 JSONL 金标用例；阈值为通过率 ≥ 0.80、Recall@K ≥ 0.70、MRR ≥ 0.60、ACL 泄漏数 = 0、P95 ≤ 5000ms。这些是配置门槛，**不是本次测出的成绩**。

## 8. 文档与源码不一致之处

**2026-10-06 P03 更新：** 下表保留原始核查清单；前五项相关文档已纠正，详见第 11 节与 P03/P04 验收指南。Rerank、前端与真实联调仍不能据此计为完成。

| 旧描述 | 当前源码事实 | 本文采用的口径 |
| --- | --- | --- |
| 已知缺口仍写“接口不接收 multipart” | 上传逻辑调用 `ParseMultipartForm` 并读取多个 `file` | 上传已实现，流式化和批量目录导入仍待补 |
| 部分说明仍写正文落到托管目录 | 知识库主路径保存/读取 `documents.content` | 数据库是正文真相，旧目录仅作兼容导入 |
| 部分说明把产品拆分与 chunk 切分都写在 worker | 产品数据集写入时拆产品，worker 对单条正文切 chunk | 区分请求期产品拆分与后台 chunk 切分 |
| 总契约注释称返回时 chunk_count 已是最终值 | 实际切块发生在后台，统计聚合分块行 | 初次响应数量不是最终索引完成量 |
| README 称默认 HTTP 端口为 8080 | 随仓库提供的 REST 配置为 8090 | 实际使用以运行时传入的 REST 配置为准 |
| 配置存在 enableRerank | 无实现或入口注入 | 不计为已完成重排 |
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

**一句话进度：主要业务代码和接口链路已经形成；当前最需要的是测试收敛、真实依赖联调，以及将已有接缝补成可上线、可持续验证的运行闭环。**


## 10. P02 配置交付补充（2026-10-06）

本节补充上述源码快照之后的配置改动，不将其等同于真实服务验收。

- API 业务配置改为 `configs/api/config.yaml`（`EINO_API_CONFIG`）；HTTP 配置改为 `configs/api/restapi.yaml`（`EINO_REST_CONFIG`）。
- worker 配置改为 `configs/worker/config.yaml`（`EINO_WORKER_CONFIG`），不再要求 API 模型、身份、HTTP 或工作区配置，也不创建工作区；两个入口不再读取 `EINO_CONFIG`。
- API 使用纯队列投递客户端；消费并发、队列权重、消费重试间隔和退出超时只放在 worker 配置。
- 已交付 `configs/acceptance/api.yaml`、`worker.yaml` 和 `restapi.yaml`，采用独立验收库 / Redis DB / collection，分别落盘 API 和 worker 日志。
- 已提供两个入口的 `-check-config`；配置及队列回归测试、入口相关包测试、两个离线配置检查均通过。
- 全量测试仍有 P01 指标相关的两项失败；未部署依赖、未跑真实端到端联调，P02 的编号执行器、实际依赖预检和安全清理仍待交付。

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
- 已新增编号执行器 `scripts/acceptance/run.sh`：P03、P04 本地/真实模式、暂停消费和投递故障模式、
  有界等待、HTTP 报告及本轮资源台账/清理。其他编号仍明确阻塞；P02 真实依赖预检未完成。
- 本地回归和生成检查已通过；真实端到端及 PostgreSQL/Milvus/ES 实际条目核对未执行。
  P01 两项指标失败仍独立待处理，不将局部测试通过写成全仓库全绿。

用户直接操作与准确的验证边界见 [P03/P04 验收指南](p03-p04-acceptance.md)。

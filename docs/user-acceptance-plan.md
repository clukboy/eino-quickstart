# Eino Harness：可逐项交付的实施与用户验收计划

> 制定日期：2026-10-06（Asia/Shanghai）  
> 依据：[当前开发进度](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/docs/current-progress.md)。  
> 本次核对的仓库 HEAD：`6dacfbd`；进度文档中的 `8887fce` 是其整理时的基线，不是当前 HEAD。  
> 范围：当前后端。未验证独立前端、真实外部依赖或生产部署；本文件最初只生成计划，P01 后续已按计划实施并单独留存验收报告。
> P01 已于 2026-10-07 完成实现；本文件的 P01 步骤保留为复测说明，当前状态以 [P01 指标与全量测试验收](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/docs/p01-metrics-acceptance.md)为准。
>
> 与现有 [详细实施计划](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/docs/implementation-plan.md)沿用相同 P01–P29 编号；原文件保留不覆盖，本文件用于执行排序、源码切入、逐步实施及用户逐项验收。

## 一、当前进度与计划边界

| 当前进度 | 本轮处理方式 |
| --- | --- |
| 对话/SSE、会话、审批、主体绑定、文档管理、异步索引、混合召回已有主要实现 | **验收已有链路**，发现问题后修复，不当作全部重做 |
| 进度快照曾记录 2 个 REST 指标测试失败；P01 已于 2026-10-07 修复 | 保留历史结论，当前以 P01 报告和全量测试结果为准 |
| Rerank、无 ES 精确匹配、目录导入等存在明确缺口 | 拆成独立功能交付，每项有正常与失败验收 |
| Skill、维护清理等已有部分代码但未完成入口接线 | 验证真正通过入口使用，不只看注册或单测 |
| 真实依赖联调、真实召回成绩、CI 门禁、迁移及索引切换未形成验收闭环 | 交付环境、报告及故障恢复证据 |
| LLM 产品拆分、外部用户同步依赖产品方案或外部资源 | 标记为条件性任务；不作为基本问答首次交付的前置条件 |

**不估算整体完成百分比，不把“接口存在”“配置开关存在”“代码合并”当作用户验收通过。** 所有编号的初始状态均为“待执行”，其中已有实现的项目含义是“待联调验收”，不是“尚未编写”。

## 二、统一交付与验收约定

### 2.1 每个计划必须交付的东西

1. **实现或修复**：范围明确，一个工作包只解决对应问题。
2. **固定样例**：包含输入、测试身份、期望结果与至少一个异常场景。
3. **可重复验收入口**：用户不改源码，执行一条命令或按明确 API 步骤复测。
4. **验收报告**：记录提交版本、工作区变更、配置摘要、实际结果、耗时、退出码及证据位置；密钥脱敏。
5. **重跑与清理说明**：重复执行不增生数据，清理只影响本轮创建的隔离测试资源。

状态流转：`待执行 → 开发中 → 开发自测通过/待用户验收 → 用户验收通过`。失败进入“需返工”，环境或数据源不足进入“阻塞”；**不能把阻塞写成通过**。

### 2.2 目前已有的命令

以下命令已存在，但需要相应环境；它们不是已经执行成功的验收证据。

```bash
export REPO="/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2"
cd "$REPO"

# P01：在允许本机临时端口监听的终端重新执行全量测试。
go test -count=1 ./...

# P02：真实联调时，在两个终端分别启动，使用隔离的配置。
# EINO_API_CONFIG / EINO_WORKER_CONFIG 分别指向 API / worker 业务配置；EINO_REST_CONFIG 指向 HTTP 配置。
go run ./cmd/worker
go run ./cmd/restapi

# P20：先按该计划准备隔离语料、索引和对应金标，再跑真实召回评测。
# EINO_API_CONFIG 必须已经指向隔离环境；报告输出父目录须预先存在。
go run ./cmd/rag-test \
  -config "$EINO_API_CONFIG" \
  -dataset "$REPO/internal/eval/datasets/retrieval.jsonl" \
  -thresholds "$REPO/internal/eval/thresholds.yaml" \
  -out "$REPO/logs/acceptance/P20/eval-report.json"
```

默认 REST 配置监听 `8090`；指标使用独立监听地址，不能把 HTTP 业务端口当作指标端口。真实联调启动会访问外部依赖，**不要直接使用示例配置连接共享或生产资源**。模型/embedding 调用可能产生费用，应在隔离环境设置预算。

### 2.3 计划交付的统一验收入口

**2026-10-07 更新：下面脚本已扩展 P01、P03、P04。** `P01` 执行全量 Go 回归并保存报告，`P01-live` 验证运行中 API 的业务健康端口与独立指标端口；未实现编号返回阻塞，不会假报通过。实际依赖预检仍是 P02 待办，配置模板不代表环境已就绪。

```bash
# 列出真正已实现的验收项。
bash "$REPO/scripts/acceptance/run.sh" --list

# 无真实依赖先复测 P04；真实 HTTP 模式需隔离资源和凭据。
bash "$REPO/scripts/acceptance/run.sh" P03
bash "$REPO/scripts/acceptance/run.sh" P04 --local
# 按 p03-p04-acceptance.md 准备环境后：
bash "$REPO/scripts/acceptance/run.sh" P04
```

验收入口需约定：成功退出码为 0；断言失败、未实现编号或前置条件不足均非 0，并明确区分失败/阻塞。每次执行输出独立报告目录，如 `logs/acceptance/<编号>/<run-id>/`；命令默认不清库、不删除共享索引、不打印凭据。人工操作无法自动化时，应生成可逐步照做的请求文件和检查表，而不是跳过断言。


### 2.4 不知道从哪里开始：先按这条路线走

**第一步不是实现 Rerank，也不是重写 Agent，而是先让现有代码有一个可信的验证起点。**

| 顺序 | 你现在做什么 | 此时不做什么 | 这一步的产物 |
| --- | --- | --- | --- |
| 1 | 看 P01 的两个现有测试，在独立终端复现失败 | 不先修改依赖版本，不跳过测试 | 能说清失败在配置、监听还是断言 |
| 2 | 按 P01 修复配置/测试约定，局部测试再全量测试 | 不接真实模型来证明单测问题 | 全量测试报告和配置选择 |
| 3 | 按 P02 列出真实依赖，确认隔离库、索引、队列和测试密钥 | 不直接启动默认配置连接现有共享库 | 一张环境清单和预检结果 |
| 4 | 先只启动 HTTP 与 worker，创建一篇小文档 | 不一次上传整个业务知识库 | 文档 ID、任务日志和 ready 状态 |
| 5 | 用这篇文档验证搜索，再做主体绑定和对话 | 不把“模型回答了一句话”当作链路通过 | 搜索结果、引用原文和 SSE 记录 |
| 6 | 固化前面步骤为验收脚本，再做产品样例和真实评测 | 不等所有增强完成才给用户验证 | 可以交给别人复跑的第一批验收包 |

如果 P01 卡住，就先处理 P01；如果 P02 没有隔离环境，就准备环境，不去用生产资源“试一下”。每一步只需要解决当前阻塞，不需要先读完所有源码。

### 2.5 看懂代码分层，避免找错修改位置

这里的“看代码”指直接阅读本仓库可维护源码及功能契约，不涉及程序逆向或破解。

| 层 | 用通俗话理解 | 第一处可打开的文件 | 应该在这里改什么 |
| --- | --- | --- | --- |
| API 契约 | 请求有哪些字段、路径是什么、返回什么 | [dataset.api](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/docs/dataset/dataset.api) | 增加/改变对外字段，先改契约 |
| Handler | 把 HTTP 请求读出来交给业务 | [创建文档 handler](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/handler/dataset/create_document_handler.go) | 解析与传输适配；不要堆索引业务 |
| Logic | HTTP 入口的业务适配 | [创建文档 logic](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/dataset/create_document_logic.go) | 组织输入、调用应用服务、转换响应 |
| 应用服务 | 真正的业务规则 | [knowledge/service.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/service.go) | 创建/更新/重建/删除、幂等与状态 |
| 后台索引 | 请求返回以后谁继续干活 | [knowledge/indexer.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/indexer.go) | 切块、embedding、写向量及索引、失败收敛 |
| 检索 | 从库里找到资料并排序 | [rag/retriever.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/retriever.go) | 通道执行、融合、重排与降级 |
| 装配入口 | 把这些零件真正接在一起 | [restapi/main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/restapi/main.go)、[worker/main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/worker/main.go) | 注入服务、工具、配置和运行生命周期 |
| 持久化 | 数据怎么保存和读取 | [数据库连接](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/storage/entx/database.go) | 连接与迁移边界；不要在生成代码里写业务 |

**修改接口的固定顺序：** 功能组 `.api` → 在隔离副本/确认可覆盖范围后运行 `make gen-api` → 适配现有 logic/handler → 局部测试 → 全量测试 → 用户样例。`types.go`、`routes.go`、Swagger 是生成物，不要只手改生成文件；`make regen-api` 会删除 handler，不能直接拿来处理当前工作区的改动。

**先记住这些词：**

- **dataset（数据集）**：一个知识库；**document（文档）**：库内一篇资料，产品库通常是一产品一文档。
- **chunk（分块）**：文档切出来的小段；**embedding**：把文本变成数字向量；**Milvus**：保存/搜索向量。
- **ES/BM25**：关键词检索；**exact**：型号等字段相等匹配；**RRF**：按多个通道名次融合；**Rerank**：对候选再精细排序。
- **worker**：独立后台进程；**Redis/asynq**：HTTP 和 worker 之间的任务队列。
- **SSE**：服务器逐段发送消息；**checkpoint**：审批中断时保存的执行状态。
- **幂等**：相同操作重复执行不多出副作用；**dry-run**：只预览，不真正修改数据。
- **Recall@K**：前 K 个结果找全了多少目标；**MRR**：正确目标排得靠不靠前；**P95**：95% 请求不超过的耗时。

### 2.6 P02 的环境清单：不是只改一个端口

配置模板从 [业务配置](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/configs/config.yaml)和 [REST 配置](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/etc/restapi.yaml)开始复制，以下每项都必须确认，不使用原地址直接试跑。

| 配置/资源 | 用途 | 隔离验收必须确认 |
| --- | --- | --- |
| `Storage.Host/Port/DBName` | PostgreSQL 正文、状态、账号与会话 | 独立验收库，允许建表，有备份/重置边界 |
| `Asynq.Redis.Addr/DB`、`Asynq.Queues` | 接收/消费索引任务 | 独立 Redis 实例或明确可隔离 DB；HTTP/worker 一致；消费队列含 `index` |
| `Milvus.Address/Collection` | 向量索引 | 独立 collection；维度与实际 embedding 输出一致 |
| `ES.Address/Index/MappingFile` | 精确与关键词索引 | 独立 index；第一轮启用 ES 跑完整链路，再单独验收无 ES |
| `Model` 与 `Embedding` | 对话模型与文本向量化 | 有有效密钥、服务额度和预算；查询与入库 embedding 一致 |
| `Workspace.Root/Knowledge.Root` | 工作区及旧文件兼容路径 | 独立验收目录；不要指向个人业务文件 |
| REST `Host/Port/Prometheus` | HTTP 和指标监听 | 业务端口、指标端口分别可用；避免占用其他服务 |
| 主体/角色密钥 | admin、agent、approver 样例 | 在本地环境变量提供，不提交、不写进报告；按端点实际角色使用 |

**关键风险：** 当前 `entx.Open` 调用 `Schema.Create`，含自动删列/删索引选项；HTTP、worker 和真实评测都可能走这个入口。P27 完成前，连库并非纯只读动作，不能用生产库跑验收。P26 的队列 trace 信封已有代码，先验证是否形成真实闭环，再决定补哪一段，不重复实现。

## 三、执行顺序与依赖总表

编号是任务标识，**不是严格执行顺序**；以依赖列为准。没有承诺交付日期，实际排期以联调和外部资源可用情况调整。

| 编号 | 小计划 | 类型 | 前置 | 用户验收时看到什么 |
| --- | --- | --- | --- | --- |
| P01 | 修复指标配置与全量测试 | 修复 | 无 | 测试全绿，指标开关行为明确 |
| P02 | 隔离环境与统一验收入口 | 基础设施 | P01 | 依赖预检、样例和编号报告 |
| P03 | 文档、契约与生成物对齐 | 收敛 | P02 | 照文档可启动，响应字段不漂移 |
| P04 | 文档写入、索引、更新与删除 | 已有链路验收 | P02 | ready、更新可查、删除不再命中 |
| P05 | 产品拆分与增量上传 | 已有链路验收 | P04、P12 | 产品数正确，重复上传不新增 |
| P06 | 混合召回、归并与降级 | 已有链路验收 | P04 | 通道、粒度和降级信息可核对 |
| P07 | 多 Agent 对话、SSE 与历史 | 已有链路验收 | P06、P09 | 流式回答、引用及历史一致 |
| P08 | 审批与检查点续跑 | 已有链路验收 | P07 | 批准前无写入，批准后仅执行一次 |
| P09 | 主体矩阵与知识库绑定 | 已有链路验收 | P04 | 双向查询一致，变更即时生效 |
| P10 | 文档启停改成显式目标状态 | 完善接口 | P06 | 重发启停请求不会反向翻转 |
| P11 | 无 ES 的结构化精确匹配 | 补功能 | P06 | 型号相等匹配，不混同子串 |
| P12 | 多文件上传与边界处理 | 完善功能 | P04 | 超限、部分失败及重传结果清楚 |
| P13 | 目录导入与增量重跑 | 补功能 | P05、P12 | dry-run、导入摘要及重复执行稳定 |
| P14 | 接入真实 Rerank | 补功能 | P20 | 本地 HTTP 契约通过；真实服务排序前后明细待隔离环境 |
| P15 | Rerank 预算、超时与降级 | 完善功能 | P14 | 本地预算/超时/降级通过；真实服务故障演练待隔离环境 |
| P16 | LLM 自动产品拆分 | 条件性增强 | P05、P20；拆分方案确认 | 无 front-matter 原文拆分正确 |
| P17 | LLM 拆分重放与并发幂等 | 完善增强 | P16 | 重传、重启、并发不增生 |
| P18 | 指定 Agent 真正使用 Skill | 补接线 | P07 | 实际调用记录，不只是工具注册 |
| P19 | 外部用户同步 | 条件性增强 | P02、P09；数据源和匹配规则确认 | 新增/更新/跳过/失败台账 |
| P20 | 真实召回基线与复测 | 质量基础 | P05、P06 | 实测指标、逐用例报告和退出码 |
| P21 | 私有语料可见性隔离评测 | 补评测 | P09、P20 | 非空隔离样例，泄漏数为零 |
| P22 | 无答案与引用正确性评测 | 补评测 | P07、P20 | 不编造，引用有对应原文 |
| P23 | CI 门禁与跨版本报告 | 持续验证 | P01、P03、P20、P21、P22 | 好版本放行，坏版本阻断 |
| P24 | 维护清理入口与 dry-run | 补接线 | P08 | 清理预览、实际计数及重跑结果 |
| P25 | 业务指标与分级健康状态 | 生产化 | P01、P04、P06 | 业务操作可观测，依赖故障可区分 |
| P26 | HTTP→队列→worker 追踪 | 生产化 | P04、P25 | 能串起同一任务的完整链路 |
| P27 | 版本化数据库迁移 | 生产化必需 | P04 | 迁移版本、历史数据与恢复证据 |
| P28 | 影子索引重建 | 生产化 | P20、P25、P27 | 新索引独立构建，旧版仍服务 |
| P29 | 索引切换、回滚与演练 | 生产化 | P28、P23 | 合格版本可切换，失败可恢复 |

### 建议按四个交付批次推进

- **第一批：能用、能复测。** `P01 → P02 → P03/P04 → P12 → P05 → P06/P09 → P07 → P08 → P20`。出口：上传、索引、召回、问答、审批均有真实证据。
- **第二批：质量可持续。** `P21/P22 → P23`；`P27` 在 P04 后尽早开展。**CI 门禁和迁移不应被可选增强拖到最后；生产发布必须通过 P23、P27。**
- **第三批：按需要补齐能力。** `P10/P11/P13/P18`，然后 `P14 → P15`；确认需求与资源后再做 `P16 → P17`、P19。
- **第四批：运行可恢复。** `P24/P25 → P26`，随后 `P28 → P29`。这些运行能力可在前置满足后提前，不要求等待所有可选功能。

## 四、每个小计划的实施与直接验收卡

阅读每项时先看“从哪里开始”，再按“按顺序做”推进；编号步骤可作为该计划的子任务逐步记录。例如 P04 第 1–6 步即 P04.1–P04.6，不要求一口气完成整个工作包。

以下每项完成后均交付同编号验收入口；用户也可以按“用户验证”复查原始 API、报告或页面。涉及已有 API 的路径统一加 `/api/v1`，健康接口除外；具体请求体、身份和固定 ID 由该项样例提供，用户不需自行猜字段。**目前未核验前端，不以点击某个不存在或未验收的页面作为通过条件。**

### P01｜修复指标配置，让全量测试全绿

- [x] 重新复现进度文档记录的两个指标测试问题，确定默认开启/关闭策略。
- [x] 同步配置、启动接线和测试；补关闭指标时不监听的验证。
- **实现状态（2026-10-07）：** P01 已完成；独立指标配置、启用/禁用测试和全量回归报告见 [P01 指标与全量测试验收](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/docs/p01-metrics-acceptance.md)。
- **用户验证：** 执行 `bash scripts/acceptance/run.sh P01`；若 API 已启动，再执行 `bash scripts/acceptance/run.sh P01-live`。
- **通过标准：** 全量测试退出码 0；开启能抓到实际指标，关闭符合约定，不能靠跳过测试转绿。
- **异常验证：** 指标端口占用要明确报错；本机监听受限标记环境阻塞，不误报业务缺陷。

#### 从哪里开始

实现切入点是 [chain_test.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/chain_test.go) 的 `ensureSetUp`、`TestShippedConfigKeepsAgentsConfigurable`、`TestPrometheusAgentServesGoZeroMetrics` 和禁用配置子进程测试；配置约定见 [restapi.yaml](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/etc/restapi.yaml)，启动时由 `Config.SetUp()` 接线。P01 已完成，以下步骤保留为实现路径记录。

#### 按顺序做

1. **先复现，不先猜。** 运行下面的局部命令，把失败输出留存，确认仍是原问题。
2. **明确约定。** 建议第一批验收明确启用独立指标端口；若产品决定默认关闭，则需要分别覆盖关闭配置和显式开启抓取，两种策略都不能留下“默认关闭却断言必须非空”的矛盾。
3. **修配置和测试初始化。** 启用时必须有 Host、Port、Path；测试可以使用临时端口，但测试中的 Path 也要明确。`ensureSetUp` 是进程内一次性初始化，不能依赖不同用例轮流覆盖全局配置。
4. **先局部绿，再全量绿。** 不移除断言，不加入无理由 Skip，不顺手升级整套依赖。
5. **补运行证据。** 启动服务后访问业务 `/health`，从独立指标端口抓指标，记录配置和两次响应。

```bash
cd "$REPO"
go test -count=1 -v ./internal/transport/restapi \
  -run 'TestShippedConfigKeepsAgentsConfigurable|TestPrometheusAgentServesGoZeroMetrics'
go test -count=1 ./...
# 启用配置使用 9091 时的抓取样例；端口以交付配置为准。
curl --fail --silent --show-error http://127.0.0.1:9091/metrics
```

**最小交付：** 已交付明确的指标配置策略、相关测试修复、全量测试输出和进程级抓取入口。真实 API 的现场抓取仍需用户在具备隔离依赖的环境启动服务后执行 P01-live。

**卡住时找哪里：** Host/Path 空→先看 YAML；连接拒绝→看独立指标端口与 `SetUp`；响应有内容但缺 HTTP 指标→先发一条业务请求，再看原生指标中间件是否记录。沙箱不允许监听时，在有权限的本地终端复测，不改测试掩盖环境限制。

### P02｜提供隔离环境与统一验收入口

- [x] 交付 API / worker 分离配置、独立覆盖变量、隔离模板、资源命名与离线检查入口。
- [x] 交付真实依赖预检实现：PostgreSQL 认证/只读探针、Redis 认证/短 TTL 写权限、Milvus collection/维度/加载状态、worker `index` 注册、可选 embedding 一次性维度探针；真实通过结果仍需用户环境复测。
- [x] 交付 [P02 配置指南](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/docs/p02-configuration.md)、`P02 --degraded/--complete`、`--list`、脱敏报告和唯一 fixture。
- [x] 交付 P02 报告目录与显式清理：`P02-cleanup REPORT_DIR --confirm` 只删除指定报告目录的 fixture；P03/P04 的业务资源清理由各自台账负责。
- **用户验证：** 按说明启动隔离 HTTP + worker；执行 `bash scripts/acceptance/run.sh P02 --degraded`，需要 ES 时再执行 `P02 --complete`；查看 `preflight.json` 和退出码。
- **通过标准：** 不修改源码即可复测；报告明确数据库、队列、Milvus collection、ES、embedding 模型/维度、API/worker/指标端口，且不含密钥；完整环境下所有检查为 PASS。
- **异常验证：** 停止 PostgreSQL/Redis/Milvus/worker 中任一项时 P02 非 0 且明确指出组件；去掉 embedding 探针不会偷偷标 PASS；未交付编号返回非 0；不得误删共享数据。

#### 从哪里开始

打开 [restapi/main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/restapi/main.go) 的 `runServer` 和 [worker/main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/worker/main.go) 的 `runWorker`，先确认它们分别读取哪些配置/变量；配置字段定义看 [config.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/config/config.go)。HTTP 通过 `EINO_API_CONFIG` + `EINO_REST_CONFIG` 配置，worker 读取 `EINO_WORKER_CONFIG`；不再读取共享的 `EINO_CONFIG`。

#### 按顺序做

1. **先填 2.6 环境清单。** 没有 PostgreSQL/Redis/Milvus 或有效 embedding 时，写清缺项；不要开始上传。只有对话验证需要真实对话模型，索引步骤主要依赖 embedding。
2. **建立隔离配置。** 已交付 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/configs/acceptance/` 下的 API / worker 独立模板；密钥只写环境变量。完成独立库、collection、index、工作区确认后才启动。
3. **先启动依赖，再启动 worker。** worker 日志应确认队列、数据库、向量服务及 embedding 可用；再启动 HTTP，确认 `/health`、`/ready`。`/ready` 当前主要探测 PostgreSQL，不能代替完整预检。
4. **做最小预检。** 执行 `bash scripts/acceptance/run.sh P02 --degraded`；若要检查 embedding 真实维度，增加 `EINO_P02_PROBE_EMBEDDING=1`，不只是测 TCP 端口。小调用会计费，需有预算。
5. **完整检索再执行 complete。** 只有配置独立 ES 后执行 `P02 --complete`；无 ES 的合法降级场景执行 `--degraded`，两者都不能绕过 PostgreSQL、Redis、Milvus 和 worker 注册检查。
6. **使用执行器和报告。** `scripts/acceptance/run.sh --list` 查看已交付入口；报告在 `logs/acceptance/P02/<timestamp>-<pid>-<mode>/`，包括 `preflight.json`、`summary.json`、日志和唯一 fixture。
7. **固化资源台账。** P02 只创建本轮 fixture 和短 TTL Redis probe key；文档/数据集/会话由后续 P04/P05 台账记录，清理只能依据台账，不做全库删除。

```bash
# 先在当前终端设置有效且已隔离的配置和密钥；不得指向未修改的默认共享地址。
# 以下模板已创建；真实依赖和有效凭据仍需自行准备。
export EINO_API_CONFIG="$REPO/configs/acceptance/api.yaml"
export EINO_WORKER_CONFIG="$REPO/configs/acceptance/worker.yaml"
export EINO_REST_CONFIG="$REPO/configs/acceptance/restapi.yaml"
export BASE="http://127.0.0.1:8090/api/v1"
# ADMIN、AGENT、APPROVER 分别保存对应验收主体的凭据，不要将值复制到文档。
# 在两个使用同一组环境变量的终端中分别运行：
go run ./cmd/worker
go run ./cmd/restapi
```

**2026-10-07 补充：** P02 已提供真实依赖预检、`--list`、脱敏报告、唯一 fixture 和 `P02-cleanup ... --confirm`；本地复测在依赖未启动时按预期 BLOCKED。P03/P04/P05/P06/P07/P08/P09/P20 已提供本地编号入口；这些项目的本地契约回归已通过，但真实业务环境联调仍需按报告边界复测。明确可使用自建/已有的隔离服务，不必先承诺制作所有组件的容器镜像。

**卡住时找哪里：** worker 不启动→先查启动日志和依赖校验；HTTP 正常但任务无人消费→检查两份配置的 Redis 地址 / DB，及 worker 的 `Asynq.Queues` 是否包含 `index`；401/403→核对样例角色而非修改权限来放行。

### P03｜统一运行文档、API 契约与生成物

- [x] 修正端口、正文存储、上传、请求期产品拆分与后台 chunk 切分等历史描述。
- [x] 对齐 `.api`、生成类型、路由与 Swagger；生成检查在隔离副本中做，不覆盖用户修改。
- **用户验证：** 从空白验收配置照文档启动；按样例调用创建、查询、搜索；对照响应字段。
- **通过标准：** 文档与实际入口一致，生成后可编译且无未解释的契约漂移。
- **异常验证：** 非法字段/参数返回可解释错误；不能直接运行强制重生成删除手改文件。

**交付状态（2026-10-06）：** 文档/契约修正、只读生成检查和编译已完成；真实启动复走未执行。详见 [P03/P04 直接验收指南](p03-p04-acceptance.md)。

#### 从哪里开始

先逐条核对进度文档第 8 节的不一致清单，再打开 [Makefile](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/Makefile)、[REST 契约入口](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/docs/restapi.api)和 [dataset.api](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/docs/dataset/dataset.api)。当前 [types.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/types/types.go) 有用户未提交修改，先保留，不直接重生成覆盖。

#### 按顺序做

1. 列一个“旧说明→真实实现→需要改的文档”对照表，先处理端口、正文数据库存储、上传和异步状态。
2. 对每个对外字段确认是不是已在 `.api` 声明；如果只出现在生成 Go 文件里，先写回契约。
3. 在隔离副本生成 API，对比 types/routes/Swagger；依赖 `goctl` 缺失则报告缺项，不直接安装升级整套工具链。
4. 必要的 handler/logic 适配手动审查；生成器不会自动补全旧 logic 的业务实现。
5. 从文档开始完整复走一次 P02/P04，特别确认初次返回 `indexing` 与计数不是最终完成量。

**用户拿到什么：** 纠正后的启动文档、接口请求/响应样例、生成差异说明；用户按文档启动不再遇到“照抄端口不对/字段丢失”。

**这一项不包含：** 改造所有接口、实现前端或强制执行 `regen-api`。契约变更需要兼容说明，不能仅“编译通过”就交付。

### P04｜文档完整生命周期

- [x] 固定小文档样例，验证创建→排队→ready→读取正文→搜索。
- [x] 验证修改正文、单文档/整库重建、删除及任务失败后的恢复。
- **用户验证：** 创建含唯一测试词的文档，轮询 ready；修改成新词并等待 ready；重建后删除，再搜索。
- **通过标准：** 正文与分块计数一致；稳定完成后新内容可查，索引正文不残留被替换内容，已删除文档不再命中；索引任务重试不增生。
- **异常验证：** 暂停 worker 后不假报完成；Redis 投递失败明确返回错误；恢复后状态有界收敛，不永久卡 indexing。

**交付状态（2026-10-06）：** 两项已在可控依赖回归中验证，并交付真实 HTTP 执行器。
- [ ] 用户环境的真实 PostgreSQL/Redis/Milvus/embedding 联调、故障演练及存储核对（不将本地回归当作这一步通过）。

**马上验证：** `bash scripts/acceptance/run.sh P04 --local`；真实模式准备与检查表见 [P03/P04 直接验收指南](p03-p04-acceptance.md)。

#### 从哪里开始

沿着这个顺序看一遍即可，不需要通读仓库：

`CreateDocumentLogic`（[HTTP logic](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/dataset/create_document_logic.go)）→ `Service.Create/enqueue`（[service.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/service.go)）→ 索引任务契约（[tasks.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/queue/tasks/tasks.go)）→ worker `registerHandlers`（[入口](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/worker/main.go)）→ `Indexer.HandleTask/IndexDocument`（[indexer.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/indexer.go)）。

#### 按顺序做

1. **先只做创建。** 新建普通 `document` 数据集，不用产品型录，正文含唯一词 `验收蓝鲸词A`，避免命中旧资料。
2. **分开看受理与完成。** 创建响应有文档 ID 即记录下来；轮询详情，等待 `ready` 且 `indexed_chunk_count == chunk_count`，非空小文档要求分块数大于 0；设置有限等待时间。
3. **再做正文与搜索。** 读取 `/content` 核对全文，搜唯一词并检查目标 `document_id`，不能只断言“有结果”。
4. **再做更新。** 用 `Service.Update` 将唯一词改为 `验收蓝鲸词B`，待 ready 后，新词能召回目标文档，返回正文和索引分块已更新，不残留旧正文。旧词查询仍可能语义召回新版本，不能仅以“旧词还有结果”判定更新失败。
5. **再做重建和删除。** 验证 `Reindex/ReindexDataset` 的受理后状态；删除后目标全文与检索结果均消失。
6. **最后做一次恢复。** 暂停 worker 后创建第二篇，确认未假报 ready；恢复 worker 继续处理。队列失败和任务失败分开记录，核对已经落库的状态及重试入口。

下面是当前已存在接口的正常路径样例，**P02 环境准备好后**才运行；需 `curl` 和 `jq`。它只帮你拿到一个最小切入点，不替代本项所有断言。

```bash
# 任何 curl 或 jq 失败时立即停止，先检查响应；不要带空 ID 继续。
DATASET_JSON=$(curl --fail --silent --show-error -X POST "$BASE/dataset" \
  -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' \
  -d '{"name":"acceptance-p04","visibility":"system","type":"document"}')
DATASET_ID=$(printf '%s' "$DATASET_JSON" | jq -er '.id')
DOC_JSON=$(curl --fail --silent --show-error -X POST "$BASE/dataset/$DATASET_ID/documents" \
  -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' \
  -d '{"title":"P04最小样例","content":"# P04样例\n\n验收蓝鲸词A只用于本次联调。"}')
DOC_ID=$(printf '%s' "$DOC_JSON" | jq -er '.data[0].id')
curl --fail --silent --show-error "$BASE/dataset/$DATASET_ID/documents/$DOC_ID" \
  -H "Authorization: Bearer $ADMIN" | jq '{id,status,chunk_count,indexed_chunk_count}'
# 先轮询上述详情确认 ready，再执行：
curl --fail --silent --show-error -X POST "$BASE/dataset/$DATASET_ID/search" \
  -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' \
  -d '{"query":"验收蓝鲸词A","top_k":5}' | jq .
```

**实现原则：** 先验收已有函数，不重写上传/索引。失败后才修改负责该阶段的层，并补对应回归测试。普通文档创建重发是否重复需要明确记录，不能把产品型号幂等规则自动套给所有文档。

**卡住时找哪里：** 没返回 ID→HTTP logic/Service.Create；一直 indexing→队列/worker；有分块却没有 indexed→`embedPending/embedBatch/indexChunks`；ready 后目标不命中→先确认索引与检索使用同一 collection/index，再看 P06。

### P05｜产品拆分与重复上传增量更新

- [x] 准备包含 3 个型号的 front-matter 型录，以及仅修改 1 个型号的版本。
- [x] 验证 `created/updated/unchanged`、产品 ID 稳定、正文及元数据变更指纹。
- **实现状态（2026-10-07）：** 本地契约验收已通过；真实 PostgreSQL/Redis/worker 联调仍需在 P02 环境执行。
- **用户验证：** 上传原样例→原样重传→修改一产品后重传；比较文档数量、ID 和操作结果。
- **通过标准：** 一产品一文档，原样重传不新增；只变更的产品更新，其他产品保持不变。
- **异常验证：** 中途失败后重传能修复，重复型号或无有效产品有明确处理结果。

#### 从哪里开始

打开 [split.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/split.go) 的 `buildProductDocuments/productKey/specFingerprint`、[service.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/service.go) 的 `createProductDocuments/upsertProductDocument/refreshProductDocument`。样例格式以 [现有铰链型录](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/tests/knowledge/H11_二段力滑入式铰链.md)和 [SplitBlocks](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/parser/product.go)为准，不凭“front-matter”一词自行更换格式。

#### 按顺序做

1. 从已有样例摘取三个完整产品块，型号改成专用 `ACC-P05-A/B/C`，数据集类型必须是 `product`。
2. 先通过 JSON `content` 验证拆分，再用 P12 的文件上传验证同一内容；对照三个 ID、型号和正文归属。
3. 等全部 ready 后原样重传，预期 `unchanged`，文档数仍为 3。
4. 只改 B 正文；再只改 B 一个元数据字段，分别确认 B 被更新而 A/C 不变。
5. 制造一条失败/未 ready 产品，再原样上传，确认它能重新排队，而非仅按正文相同就跳过。

**局部自测入口：** `go test -count=1 ./internal/application/knowledge`；已有 `split_test.go/service_test.go` 用于补回归，真实数据库幂等仍要验收。

**用户验收产物：** 三次上传前后对照表：型号、ID、operation、状态、正文/元数据摘要、数量。核心是“只改变有变化的产品”，不只是“返回了三行”。

### P06｜三通道召回、结果归并与降级

- [x] **本地契约验收（2026-10-07）：** `bash scripts/acceptance/run.sh P06` 已通过；覆盖通道成功/失败降级、精确匹配、归并、粒度、预算和截断元数据。
- [ ] **真实依赖验收：** PostgreSQL/Milvus/ES 及固定语料环境待复测。
- [ ] 准备型号、关键词、语义问题样例，分别验证 exact/keyword/vector 及融合结果。
- [ ] 对照 `channels/degraded/granularity/matched_chunks/chunk_budget` 验证 document/chunk 归并。
- **用户验证：** 调用 `POST /dataset/:id/search`；记录正常结果，再关闭 ES 配置或模拟依赖故障后重跑。
- **通过标准：** 通道状态真实，返回数量与粒度可解释，故障不能包装成“确实没有答案”。
- **异常验证：** 单通道失败按已定义策略降级；所有通道不可用给明确失败，不伪造空成功。

#### 从哪里开始

先看 [retrieval.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/restapi/retrieval.go) 的 `newRetrievalSearcher`，确认入口装了哪些依赖；然后看 [HybridRetriever.RetrieveDetailed](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/retriever.go)、[Store.SearchByExact/SearchByText/SearchByVector](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/store.go)和 [Service.Search/recall/settle](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/search.go)。

#### 按顺序做

1. 复用 P04/P05 的已 ready 样例，分别选型号、正文关键词、换一种说法的语义问题，不引入新导入方式。
2. 完整配置下记录每个通道候选及融合结果；`channels` 表示真正给出候选的通道，不能要求每一个问题都固定返回全部三种通道。
3. 分别测 `product` 数据集按 document 归并、普通库按 chunk 归并；对照同一文档多个 chunk 是否占满结果。
4. 关闭 ES 配置并重启隔离 HTTP，验证 PG 回落和 exact 当前降级；之后再测一个已运行依赖临时故障。启动时关闭的通道和请求中故障须分别说明。
5. 加小/大 `top_k` 及返回正文截断样例，核对预算、条数、粒度、`content_truncated`。

**局部自测：** `go test -count=1 ./internal/rag ./internal/application/knowledge`。

**用户验收产物：** “输入问题→通道→候选→融合顺序→最终 document/chunk”对照报告；若向量命中差，先验证同一 embedding 模型/维度，而不是盲调所有权重。

### P07｜多 Agent 对话、SSE 与历史

- [x] **本地协议验收（2026-10-07）：** `bash scripts/acceptance/run.sh P07` 已通过；覆盖路由、请求校验和 SSE 帧格式。
- [ ] **真实模型验收：** 多 Agent 回答、引用、历史和断流待复测。
- [ ] 固定知识问答和只读工作区样例，核对路由、流式终态与引用。
- [ ] 验证会话历史、超时、客户端断连后的执行与持久化边界。
- **用户验证：** 创建会话，使用不缓冲的 SSE 客户端请求 `POST /chat`；再查询会话消息，并打开被引用正文。
- **通过标准：** 可见增量与明确终态；消息历史一致；引用指向真实文档/章节/chunk。
- **异常验证：** 模型超时、无知识库授权、断连均有可解释结果，不生成重复轮次或静默挂起。

#### 从哪里开始

从 [ChatLogic.Chat](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/stream/chat_logic.go)开始，确认会话/运行准备和 SSE 输出；Agent 分配看 [NewHarness](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/agent/factory.go)、[root.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/agent/root.go)；知识工具看 [knowledge.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/tool/knowledge.go)。

#### 按顺序做

1. 完成 P09 后，创建一个会话，先问 P04 样例中确实有答案的问题，不用开放式业务问题。
2. 用 `curl -N` 保存 SSE 原始数据；事件种类在 JSON `type`，不是 HTTP `event:` 行。
3. 核对 `message` 增量、`done/error/approval_required` 对应终态，以及是否调用了知识工具。
4. 用同一个 `session_id` 再问一个承接问题，查询 `/sessions/:id/messages` 对照用户和助手消息。
5. 做一次断连和模型超时，记录 run/turn 最终状态；流开始后错误可能仍是 HTTP 200，必须看 `type=error`。

```bash
SESSION=$(curl --fail --silent --show-error -X POST "$BASE/sessions" \
  -H "Authorization: Bearer $AGENT" | jq -er '.session_id')
curl -N "$BASE/chat" -H "Authorization: Bearer $AGENT" \
  -H 'Content-Type: application/json' \
  -d "{\"session_id\":\"$SESSION\",\"message\":\"P04样例的唯一验收词是什么？请给出资料来源\"}"
```

**最小交付：** 正常知识问答、只读工作区、承接历史和一个异常场景各一条可复测记录；只读工作区不得授予写入权限来让演示成功。

### P08｜审批、拒绝与检查点续跑

- [x] **本地状态机验收（2026-10-07）：** `bash scripts/acceptance/run.sh P08` 已通过；覆盖审批、检查点、批准、拒绝边界、Claim/Consume 单次保护和审计脱敏。
- [ ] **真实 HTTP/Agent 验收：** 隔离 workspace 写入审批和重启恢复待复测。
- [ ] 提供仅在隔离工作区写入测试文件的审批样例，覆盖列表、筛选、详情和决策。
- [ ] 验证通过/拒绝、过期及重复/并发续跑。
- **用户验证：** 发起操作，审批前检查文件不存在；批准并续跑，确认文件生成；另起请求拒绝；重复续跑。
- **通过标准：** 审批前无副作用，批准后只执行一次，拒绝不执行；检查点能恢复原操作。
- **异常验证：** 过期、重复决策或已完成续跑不能重复执行；报告说明每次拒绝原因。

#### 从哪里开始

打开 [decide_approval_logic.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/approver/decide_approval_logic.go)、[ResumeApprovalLogic.ResumeApproval](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/stream/resume_approval_logic.go)，再看 [approval.Store](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/persistence/approval/approval.go) 的 `Decide/ClaimForResume/Consume` 和 [checkpoint store](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/persistence/checkpoint/store.go)。

#### 按顺序做

1. 在隔离 workspace 准备“写一个测试文件”的确定性请求，确保策略要求审批；不使用删文件或执行任意命令来演示。
2. 收到 `approval_required` 后记录 approval/session ID，检查文件尚不存在；用 approver 身份查看列表、筛选和详情。
3. `POST /approvals/:id/decision` 用 `{"approved":true}` 批准，再按续跑契约发请求，检查终态和一次写入。
4. 独立另起一条请求用 `{"approved":false}` 拒绝，确认无写入。
5. 重复/并发调用续跑、测试过期批准，核对 Claim/Consume 保护与审计记录；无需重新实现已有的状态机。

**用户验收产物：** 每条审批的“申请→审批前文件→决策→续跑→审批后文件→重复调用结果”表，明确用于决策/续跑的身份。

**卡住时找哪里：** 没触发审批→工具策略/Agent；找不到检查点→中断保存；已批准却不能续跑→Claim 状态与检查点；重复写→Consume/并发原子性。

### P09｜主体矩阵与知识库绑定即时生效

- [x] **本地即时生效验收（2026-10-07）：** `bash scripts/acceptance/run.sh P09` 已通过；知识工具每次调用都会重新解析当前主体绑定。
- [ ] **真实身份矩阵验收：** 主体管理 API、双主体对话和解绑后的下一次请求待复测。
- [ ] 固定两个主体及一个隔离知识库，对照主体列表、绑定查询与数据集反查。
- [ ] 通过对话入口验证新增/移除绑定在后续请求中生效，而非只验证管理 API。
- **用户验证：** 绑定主体 A→双向查询→A 提问；移除绑定后再次提问；对照未绑定的 B。
- **通过标准：** 正反查询一致；移除后不能继续召回该库；B 始终不可见。
- **异常验证：** 未授权身份不能修改绑定；跨主体请求不能借客户端传入的库 ID 绕过授权。

#### 从哪里开始

打开 [grant_agent_dataset_logic.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/admin/grant_agent_dataset_logic.go)、[revoke_agent_dataset_logic.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/admin/revoke_agent_dataset_logic.go)、[list_agent_subjects_logic.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/admin/list_agent_subjects_logic.go)、[list_dataset_agents_logic.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/admin/list_dataset_agents_logic.go)。动态解析知识库白名单的入口继续看 [restapi/main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/restapi/main.go) 和 [知识工具](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/tool/knowledge.go)。

#### 按顺序做

1. 从主体列表取得两个真实 subject，不自编一个字符串；记清 subject 与 bearer 凭据对应关系。
2. 使用 admin 将 P04 数据集绑定给 A：`PUT /agents/:subject/dataset/:id`；查询 A 的库与该库主体反查。
3. A/B 分别通过对话提同一个唯一词；B 不应因数据集是 system 就自动拥有所有绑定。
4. 删除 A 绑定后，在原进程不重启的情况下再次对话；确认工具每次按当前主体重新解析授权。
5. 补重复绑定/解绑和非法数据集样例，确认结果幂等且矩阵一致。

**用户验收产物：** 绑定前/后/解绑后的矩阵与 A/B 对话结果；管理搜索接口需要 admin，不能拿 admin 搜索成功当作主体 A 已获得对话授权。

### P10｜文档启停使用显式目标值

**状态：本地契约已完成；真实 HTTP/重启联调待复测。**

- [x] 将翻转式接口改成明确设置 `enabled=true/false`，并明确旧客户端必须迁移到显式目标值。
- [x] 使用数据集 ID + 文档 ID 联合定位，跨数据集父子路径不匹配时拒绝修改。
- [x] 验证检索遵守启停过滤：停用不召回，启用后可召回；正文内容不被删除。
- [ ] 在真实 API 进程重启后复核状态持久化，并补 PostgreSQL/检索服务联调记录。

**用户验证入口：**
```bash
bash scripts/acceptance/run.sh P10
```

**手工 HTTP 验证：** 对同一文档发送 `PATCH /api/v1/dataset/{id}/documents/{docId}/enabled`，请求体依次使用 `{"enabled":false}` 两次、`{"enabled":true}` 两次；读取正文并搜索固定词。缺失 `enabled` 必须报错，使用另一数据集路径操作该 `docId` 必须被拒绝。

**通过标准：** 重发停用仍停用，重发启用仍启用；停用内容不召回但正文仍可读；启用后恢复召回；跨数据集请求不改变原文档。

**当前实现入口：** [EnableDocumentLogic](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/dataset/enable_document_logic.go)、[Service.SetEnabled](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/service.go)、[dataset.api](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/docs/dataset/dataset.api)。

**本地验收产物：** `logs/acceptance/P10/<timestamp>-local/tests.log` 与 `summary.json`；真实外部依赖和重启验证需单独记录，不能用本地测试冒充。

### P11｜无 ES 时的结构化精确匹配

- [x] 复用 mapping 声明，在 PostgreSQL metadata 上实现真正相等匹配，并统一大小写/空格规范化。
- [x] 区分结构化精确与 PG 子串关键词通道，覆盖 `ACC-H105P`、`ACC-H105PE`、部分型号和不存在型号。
- [x] 覆盖 enabled、dataset、variants 过滤，以及 exact 进入融合和通道台账。
- **用户验证：** 先运行 `bash scripts/acceptance/run.sh P11`；再关闭 ES，导入 `ACC-H105P` 与 `ACC-H105PE`，查完整型号、部分型号和不存在型号。
- **通过标准：** 完整型号进入 exact；近似型号不被标成 exact；子串只能由 keyword 返回；关闭 ES 不会让功能静默消失。
- **边界：** PG 回落为正确性优先的 metadata 扫描，真实 API、数据库重启和规模性能仍需隔离环境复测。

#### 从哪里开始

从 [Store.SearchByExact](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/store.go)开始：当前精确索引缺失会返回降级；PG 访问入口是 [Postgres.SearchChunks](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/store/postgres/chunk.go)。先区分“元数据能做 substring”与“真正型号相等”。

#### 按顺序做

1. 先选最小字段范围：第一版只承诺 `product_id/model`；系列、品类需要另有匹配语义，不一开始就泛化全元数据。
2. 定大小写、首尾空格规范化，构造 `ACC-H11/ACC-H110` 的金标对照。
3. 给 PG 增加结构化字段相等查询（不要复用 LIKE），保留 enabled、dataset、visibility 等现有过滤。
4. 在 `SearchByExact` 接入 PG 回落，明确报告“无 ES 但有 PG exact”；HTTP/评测链路均使用同一逻辑。
5. 以元数据查询正确性为第一目标；是否需索引优化根据实际查询和数据量另做验证，不能直接宣称性能已达标。

**用户验收产物：** ES 关闭配置、字段规则及近似型号候选明细；部分串可以由 keyword 命中，但不能冒充 exact。

### P12｜多文件上传、大小限制与部分失败

- [x] 已定义并接入格式、单文件/总请求大小、最多文件数和内存边界。
- [x] 已覆盖合法多文件、空文件、超限、坏格式、缺字段和成功/失败混合。
- [x] 已交付逐文件 `files[]` 台账与 `partial_failure`；失败文件可单独重传。
- **用户验证：** 先运行 `bash scripts/acceptance/run.sh P12`；再在真实 API/worker 环境执行本计划中的 curl 步骤并保留响应。
- **通过标准：** 成功文件可索引；每个失败明确 `code/message`；成功项不因同批失败而丢失；临时文件不持续增长。
- **异常验证：** 不因一个失败文件假报整批成功；读取超限内容前按约定拒绝；不宣称普通文档任意重传都幂等。

#### 已确定的契约

| 项目 | 口径 |
| --- | --- |
| multipart 文件字段 | `file`，允许重复 |
| 支持格式 | `.md`、`.markdown`、`.txt`、`.text`；不含 PDF/Office |
| 单文件上限 | `Knowledge.MaxDocumentBytes`，默认 5 MiB |
| 请求总上限 | `Knowledge.MaxUploadRequestBytes`，默认 50 MiB，包含 multipart 开销 |
| 文件数量上限 | `Knowledge.MaxUploadFiles`，默认 10 |
| 内存边界 | `ParseMultipartForm(8 MiB)` 仅控制内存/临时文件切换；实际文件读取再用 `max+1` 限制 |
| 全部文件成功 | HTTP 200，`partial_failure=false`，`files[].status=succeeded` |
| 部分失败 | HTTP 200，`partial_failure=true`；`data` 只列成功文档，`files[].error` 列失败原因 |
| 请求级失败 | 缺 `file`、multipart 损坏、请求总量/文件数超限，返回 400/413 错误 envelope |
| 重传边界 | 只重传 `status=failed` 项；普通文档没有额外稳定键时，不承诺任意重传绝对幂等 |

#### 用户直接验证

1. 运行 `bash scripts/acceptance/run.sh P12`；报告在 `logs/acceptance/P12/<时间>-<pid>-local/`。
2. 准备两个正常小文件：`curl -X POST "$BASE/dataset/$DATASET_ID/documents/upload" -H "Authorization: Bearer $ADMIN" -F "file=@样例A.md" -F "file=@样例B.txt"`。
3. 确认返回有 `data` 与两个 `files[].status=succeeded`；等待 worker 完成后，用文档列表和搜索核对正文标记。
4. 将空文件、`.pdf` 和超过单文件上限的文件与一个正常文件混传；确认正常文件仍成功，失败项分别报告 `empty_file`、`unsupported_format`、`payload_too_large`。
5. 只上传失败项进行修复；检查文档列表和搜索，确认没有因为“整批重传”产生不必要的新文档。

#### 从哪里开始（源码已变更）

入口是 [UploadDocumentLogic.UploadDocument](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/dataset/upload_document_logic.go)。配置字段在 [KnowledgeConfig](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/config/config.go)，接口契约在 [dataset.api](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/docs/dataset/dataset.api)。

实现要点：请求先检查总量和文件数量；逐文件校验扩展名、空内容与字节上限；成功文件调用既有 `knowledge.Service.Create`，失败文件写入逐文件错误；请求结束统一 `MultipartForm.RemoveAll`。

### P13｜目录批量导入与增量重跑

- [x] 交付独立 `cmd/knowledge-import` 入口、递归/排除规则、稳定 source 和 dry-run。
- [x] 交付新增/更新/跳过/失败摘要及 `--retry-report` 可恢复重跑；源文件删除默认只报告、不删库。
- **用户验证：** 先 dry-run，再导入固定目录；原样重跑；修改一个文件再次导入。
- **通过标准：** 预览不写数据，实际计数可对照；重跑不重复，只更新变更项。
- **异常验证：** 不可读文件不导致其他结果丢失；路径越界拒绝；缺省不因源文件缺失删除知识库数据。

#### 从哪里开始

已交付独立 CLI，而不是复杂页面：入口为 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/knowledge-import/`，复用 [Service.Create](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/service.go) 和 `ImportDirectory`；不复制完整摄取代码。

#### 按顺序做

1. 命令契约已落地：`-root`、`-dataset`、`-exclude`、`-dry-run`、`-report`、`-retry-report`。
2. dry-run 先查询现有文档并输出相对路径→稳定 source→内容指纹映射，不调用写库/排队。
3. 正式导入逐文件记录 created/updated/unchanged/failed，错误文件不丢失已完成台账。
4. 普通文档用数据集 + 相对路径主干 + 路径哈希生成稳定查重键；正文 SHA-256 决定是否更新。
5. 选含嵌套目录、重复文件名、一个坏文件的样例，验证原样重跑、单文件修改、失败恢复；源文件删除默认不删库。

**用户验收产物：** 可复制的 CLI 命令、固定目录树、dry-run 与实际执行摘要，来源路径稳定且不超出导入根目录。

### P14｜真实 Rerank

- **本地实现状态（2026-10-07）：** 已交付供应商中立 HTTP 适配器、API 组合根注入和 P14 本地验收入口，详见 [P14 验收说明](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/docs/p14-rerank-acceptance.md)。真实供应商与模型效果仍需隔离环境。

- [x] 实现重排适配器并在实际入口注入，提供开关及排序前后明细。
- [x] 本地固定候选已覆盖关闭/开启适配器的排序契约；真实同语料质量对照待隔离服务。
- **用户验证：** 复测 P20 固定样例，查看候选旧排序、新排序、打分和真实服务调用证据。
- **通过标准：** 开关影响真实调用，指定用例达到预设目标且总体无未解释退化；“调用成功”不等于“质量提升”。
- **异常验证：** 结果长度、分数或索引非法不能静默进入最终答案。

#### 从哪里开始

现有接缝是 [Reranker 接口和 HybridConfig.Reranker](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/retriever.go)，调用发生在融合后、topK 截断前；真实入口看 [newRetrievalSearcher](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/restapi/retrieval.go)。只改 `Retrieval.EnableRerank` 开关不会产生真实重排。

#### 按顺序做

1. 先取得 P20 基线，再确认重排服务协议、模型、密钥、成本和需要发送的最小文本；未确定服务时先写适配契约，不假定任意服务兼容。
2. 计划新增 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/rerank/` 适配器，实现已有接口，保留原文档 ID 和来源，不用新文档替换候选身份。
3. 在 HTTP 组合根及 rag-test 评测装配中按开关注入，保证测到的就是业务使用的链路。
4. 先用确定性测试响应验证顺序/分数/非法返回，再做真实服务样例。
5. 同一语料和候选做开关对照，提前指定难例的目标；记录费用/延迟，不能只追求某条更好而隐瞒总体回退。

**用户验收产物：** 真实调用证据、排序前后对照及 P20 指标变化；P15 完成前不把无边界重排直接作为生产默认。

### P15｜Rerank 候选预算、超时与降级

- **本地实现状态（2026-10-07）：** 已完成候选预算、超时、非法结果校验和 fail-open 降级，验收入口为 `bash scripts/acceptance/run.sh P15`，详见 [P15 验收说明](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/docs/p15-rerank-acceptance.md)。真实服务故障演练待隔离环境。

- [x] 真正消费候选上限，设置超时/取消与回落到融合排序策略。
- [x] 报告调用候选数和降级原因；调用耗时由独立超时和验收日志核对。
- **用户验证：** 降低预算确认实际发送数量；让重排服务超时/返回错误，再复测搜索。
- **通过标准：** 不超过候选预算；在约定时限内结束；降级结果与原因可查看。
- **异常验证：** 预算非法不能忽略；服务故障不造成无限等待或整个请求不可解释失败。

#### 从哪里开始

看 [RetrieveDetailed 的重排分支](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/retriever.go)和 [RetrievalConfig](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/config/config.go) 的候选预算。当前重排报错会直接返回错误，不能凭有 `degraded` 字段就假定已有重排降级。

#### 按顺序做

1. 用 `Retrieval.MaxRerankCandidates` 截取重排候选，写清超过预算部分是否保留以及最终合并规则。
2. 设置独立超时和调用取消，明确总检索预算与重排预算的关系。
3. 调用前保留原融合排序，报错时回落原排序；新增可查看的降级信息与指标。
4. 对超时、服务错误、空返回、重复/非法候选序号、调用方取消分别测试；结果合法性不能只检查 HTTP 200。
5. 用候选数超过预算的固定样例验证实际发送数量、有限结束时间和回落结果。

**用户验收产物：** 候选预算/超时规则、实际发送数和降级响应；阈值值由环境确认，不在计划里虚构性能保证。

### P16｜LLM 自动产品拆分（条件性）

- [ ] 先确认产品边界、输出结构和模型预算，再实现无需 front-matter 的拆分入口。
- [ ] 对照已有结构化解析路径，保留正文、来源、元数据及失败回退边界。
- **用户验证：** 上传有人工金标的原始型录，逐项对照产品数、型号、正文归属和字段。
- **通过标准：** 满足预先确定的金标标准，不串产品、不凭空补字段；结构化样例不回退。
- **异常验证：** 模型超时、结构非法或无法判定产品时明确失败/待确认，不伪造成功。

#### 从哪里开始

当前 [buildProductDocuments](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/split.go) 调用 [SplitBlocks](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/parser/product.go) 做结构化拆分。不要直接用 LLM 替换所有解析；先把“结构化解析/自动识别”的可选策略边界定义出来。

#### 按顺序做

1. 产品负责人提供一份无需 YAML 的原文及人工正确拆分；确认跨页续段、共用规格、无型号产品如何处理。
2. 定输出 schema：稳定产品键、标题、正文范围、元数据、待人工确认项；不让模型补原文没有的数据。
3. 增加拆分策略接口，把 LLM 输出转换为已有产品规格，再复用 Service 的写入/排队链路。
4. 设置输入长度、调用预算、超时、结构校验；在数据库写入前验证完整结果，失败不落下一批不确定产品。
5. 保留结构化原路径，运行同一金标的正常、失败、疑难输入，确认无 YAML 才按约定进入自动路径。

**用户验收产物：** 原文与人工金标、模型实际输出及每产品对照；若金标/边界未确认，该项保持条件阻塞，不以“模型返回 JSON”算完成。

### P17｜LLM 拆分重放与并发幂等

- [x] 已交付输入/模型/解析版本指纹、版本元数据、稳定产品键以及现有唯一约束 upsert 复用。
- [x] 已交付本地重复/版本变化/增量更新契约。
- [ ] 真实 API/DB/queue 并发、重启和显式重解析仍待联调。
- [ ] 固定模型/提示/拆分版本及可复用结果，定义稳定产品键。
- [ ] 验证并发同文档上传、部分失败、进程重启与原样重传。
- **用户验证：** 同一样例重复上传、同时上传、重启后上传；对照 ID、数量与操作台账。
- **通过标准：** 同一版本相同输入不会增生产品或无故重嵌入；真实变化能被识别。
- **异常验证：** 部分模型输出或任务失败可重试；版本升级显式可追溯，不能悄悄换指纹算法。

#### 从哪里开始

现有稳定键/指纹在 [productKey/specFingerprint](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/split.go)；数据库唯一约束看 [document schema](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/ent/schema/document.go)；并发写入复用 [upsertProductDocument](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/service.go)。

#### 按顺序做

1. 固定拆分模型、提示与 schema 版本，定义缓存键和过期/升级语义；相同输入不能每次重新随机构造产品 ID。
2. 持久化可重放的拆分结果或稳定计划，别只放在进程内存；正文和元数据规范化后再计算指纹。
3. 用唯一约束和冲突重查保护并发，不能只用“先查没有再创建”；记录已有文档的可恢复进度。
4. 两个请求同时上传同输入，停止进程后重启重传，再对半完成结果恢复；确认最终产品/任务数量。
5. 版本升级必须明确是否重拆与重索引，验收旧结果可追溯，而不是每次变提示就静默改变数据。

**用户验收产物：** 输入/策略版本/产品键/文档 ID 的多轮对照，证明重复与并发不会增生，真正变化仍能更新。

### P18｜指定 Agent 真正使用 Skill

**状态：本地 Agent 工具授予与权限边界已完成；真实模型调用和工具记录待验证。**

- [x] 已明确只向 `workspace_agent` 授予 `list_skills`、`load_skill`，并保留既有读取/写入/执行权限边界；root coordinator 继续通过 specialist 委派。
- [x] 已提供本地静态回归和固定 Skill 加载/越界边界；Skill 内容只提供指导，不自动扩大工具权限。
- [ ] 真实模型调用、工具调用顺序和演示输出仍需隔离环境验证。
- **用户验证：** 先运行本地 P18；真实环境再提交需使用演示 Skill 的请求，查看对应工具记录及输出，再移除 Skill 重跑。
- **通过标准：** 本地专项 Agent 契约通过；真实环境还需证明 Skill 真正影响执行，不以“工具已经注册”代替入口可用。
- **异常验证：** 不存在、格式错误或越界 Skill 清晰报错，不扩大 Agent 权限。

#### 从哪里开始

工具定义看 [skill/loader.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/skill/loader.go)；全局注册看 [restapi/main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/restapi/main.go)；实际授予看 [agent/factory.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/agent/factory.go)、[knowledge.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/agent/knowledge.go)、[workspace.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/agent/workspace.go)、[automation.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/agent/automation.go)。本地回归入口为 `bash scripts/acceptance/run.sh P18`，详细边界见 [P18 验收说明](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/docs/p18-skill-acceptance.md)。

#### 按顺序做

1. 先选择一个 Agent 与一项只读演示 Skill，例如按指定格式总结测试资料；不同时给所有 Agent 加权限。
2. 根据 loader 真实工具名把工具加入该 Agent 的集合，更新提示使模型知道何时查询/读取 Skill。
3. 加一个确定性集成测试确认授予，再做真实模型调用确认工具真正触发。
4. 移除 Skill、提供非法名称或越界请求，确认失败清晰；Skill 内容不能越过审批和 workspace 策略。
5. 对默认 Agent 无需 Skill 的任务做回归，不把所有任务强制变成读 Skill。

**用户验收产物：** 演示 Skill 文件、调用记录、输出差异与适用 Agent 清单，不仅是一段“已支持 Skill”的配置。

### P19｜外部用户同步（条件性）

- [x] 本地 provider-neutral HTTP source、`external_id` 稳定匹配、dry-run、created/updated/skipped/failed 台账、幂等重跑与 no-delete 边界已实现。
- [x] 同步账号一次性初始密码与首次改密边界已实现；上游不能覆盖本地 role/status/password；手工同名账号不会被自动认领。
- [ ] 真实上游验收仍待 source endpoint、协议/分页、稳定 ID、验收凭据和密码下发方案确认。
- **本地直接验证：** `bash scripts/acceptance/run.sh P19 --local`，打开报告 `tests.log` 与 `summary.json`。
- **真实直接验证：** 条件具备后新增一名用户，先 dry-run 再正式同步；改昵称后再次同步；原样第三次同步；模拟上游不可达、重复 ID、用户名冲突和暂时少人，确认错误有台账且不删除本地用户/会话。
- **通过标准：** 本地契约通过；真实环境中稳定外部 ID 不重复建号，缺人不删除，凭据不进入日志和报告。

### P20｜真实召回基线与一键复测

- [x] **本地评测契约验收（2026-10-07）：** `bash scripts/acceptance/run.sh P20` 已通过；覆盖 JSONL 金标校验、阈值门禁、指标计算、失败记录和逐用例报告渲染。
- [ ] **真实召回基线：** 按当前金标装载隔离语料，确认索引 ready，记录模型、配置、版本、通道和归并粒度。
- [ ] 执行 `cmd/rag-test`，归档逐用例结果与整体指标；提供报告父目录和准备命令。
- **用户验证：** 先执行本地 P20 入口并打开 `summary.json`；真实依赖准备后再按交付说明执行评测命令，读取 JSON 与退出码，再重复执行。
- **通过标准：** 采用当前配置门槛：通过率 ≥0.80、Recall@K ≥0.70、MRR ≥0.60、ACL 泄漏=0、P95 ≤5000ms；这些是门槛，不是本地测试或当前工作区的实测成绩。
- **异常验证：** 缺语料/旧索引/用例不匹配必须报错或阻塞；故意构造不达标样例时非 0 退出。通道或粒度不同不可直接混比；只有本地契约通过不能标记真实基线完成。

#### 从哪里开始

入口看 [rag-test/main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/rag-test/main.go) 的参数与预检；用例字段看 [eval/types.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/eval/types.go)，执行/指标看 [runner.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/eval/runner.go)与 [metrics.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/eval/metrics.go)，门槛看 [thresholds.yaml](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/eval/thresholds.yaml)。

#### 按顺序做

1. 先列现有金标实际需要的语料/source；当前用例带历史语料前提，不能对空验收库直接跑。缺对应语料就重新构建可追溯金标，不能降低门槛让空库通过。
2. 在隔离库导入样例，等待 ready，记录实际 dataset、source、模型/维度、mapping 与数据集类型。
3. 确认评测过滤范围以及可见性；source 不稳定时选择字段支持的关键词期望，并记录其宽松边界。
4. 创建报告父目录，再跑一次 `-v` 真实评测；保留所有失败明细，修数据/链路后重新跑，不手改结果。
5. 复测一次，解释延迟波动；跨环境/通道/粒度变化另开基线，不直接和旧数字比较。

**一个容易误判的点：** 现有 `min_results=0` 无答案用例只能验证检索没有报错，不能证明生成回答没有编造，生成质量要在 P22 验收。Recall/MRR 与 `top_k`、归并单位的报告口径也必须随报告注明。

**用户验收产物：** 可准备语料的步骤、对应 JSONL、实际 JSON 报告、命令退出码及失败用例；阈值是门槛，不是本次成绩。

### P21｜私有语料可见性隔离评测

- [x] **本地隔离契约验收（2026-10-07）：** `bash scripts/acceptance/run.sh P21` 已通过；包含两份非空合成私有 source、正向/forbidden 对照、注入泄漏失败和动态主体绑定刷新。
- [ ] **真实主体矩阵验收：** 增加真实隔离环境中的非空私有文档，覆盖召回、引用和对话入口。
- **用户验证：** 先查看 P21 `summary.json` 和 `tests.log`；真实环境中再分别用主体 A/B 查询各自唯一词，检查另一方 source、正文、引用和元数据都不出现。
- **通过标准：** 本地注入泄漏时非零退出；真实正向样例可命中，未授权返回不含私有内容，ACL 泄漏严格为 0。
- **异常验证：** 修改调用方指定的数据集、owner 或 visibility 不能扩大服务端已授予范围；空库不能作为隔离通过证据。

#### 从哪里开始

打开 [Case 字段](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/eval/types.go) 的 `allowed_sources/forbidden_sources` 及 [Runner.Run](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/eval/runner.go)。既有金标没有有效私有语料对照，应新增独立的 private 金标，不是给原报告加一列 0。

#### 按顺序做

1. 实际写入两条含不同唯一词的私有样例，属主/绑定分别对应 A/B，授权正向查询先验证可命中。
2. 用真实 source 定 forbidden，分别构造 A可见/B不可见与解绑后的对照；明确字段过滤与服务端主体解析各测什么。
3. 在 rag-test 测过滤正确性；在对话入口测实际身份解析，不把手填 `allowed_sources` 等同完整授权验收。
4. 每条泄漏以返回的 document/chunk/source 和摘要记录；最小有效正向/反向样例数必须非 0。
5. 纳入 P23 报告并坚持泄漏阈值 0；错误/跳过也不能变成“无泄漏通过”。

**用户验收产物：** 私有文档确实存在的证据、A/B 对照请求与隔离报告；未授权用户无权限阅读时，不通过其他管理端接口替代验证。

### P22｜无答案与引用正确性

- [x] **本地回答证据契约验收（2026-10-07）：** `bash scripts/acceptance/run.sh P22` 已通过；覆盖必需事实、禁止事实、明确无答案、引用解析和授权 source 校验。
- [ ] 准备真实模型上的有答案、完全无答案、相似型号易混淆样例及逐条判定标准。
- [ ] 核对回答论断与来源文档/章节/chunk，不以有引用格式代替有事实证据。
- **用户验证：** 先执行 P22 本地入口；真实模型环境中逐条提问并打开引用原文，对照断言，确认无答案时明确说明依据不足。
- **通过标准：** 本地缺引用/越权引用/编造无答案失败；真实关键论断均有证据支持，错型号内容不能用于回答目标型号。
- **异常验证：** 召回故障、无授权与确实无答案分开呈现；不能统称“知识库没有”。

#### 从哪里开始

从 [知识工具的引用格式化](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/tool/knowledge.go)和 [ChatLogic](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/stream/chat_logic.go)开始。现有 `internal/eval` 主要是检索评测；生成回答评测应独立定义，不篡改 Recall 语义。

#### 按顺序做

1. 从固定语料列“问题→必须出现的事实→证据 source/章节→不得出现的错误事实”清单。
2. 再列无答案、近似型号与依赖故障问题，三类分别有期望行为；答案文本不要求逐字一致。
3. 自动检查引用 ID 可解析、来源属于授权候选、关键结构和终态；事实支持用人工对照或可审计评分辅助。
4. 若使用模型裁判，固定版本/规则并保留证据，不能把裁判输出当唯一确定性真相；有争议项交人工复核。
5. 输出逐问题回答和证据表，明确通过门槛、复核人及哪些项可以接 CI 自动阻断。

**用户验收产物：** 直接照问的题单、实际回答、可打开引用与判定记录；“生成了一段带引用的话”不算引用正确。

### P23｜CI 门禁与跨版本报告

- [x] **本地质量门禁验收（2026-10-07）：** `bash scripts/acceptance/run.sh P23` 已通过；串行运行全量 Go 回归、API 契约、P20、P21、P22，并输出分项日志和 `summary.json`。
- [ ] 将真实召回和必要的回答质量检查接入确定的 CI 平台，管理隔离服务依赖、凭据、报告归档和跨版本配置指纹。
- **用户验证：** 先执行本地 P23；确定 CI 平台后再触发一次正常流水线，并提交一个必然失败的评测用例验证门禁。
- **通过标准：** 本地代码/契约门禁失败返回非零；真实 CI 中代码测试和质量评测均通过才放行；失败有明确原因和报告。
- **异常验证：** 依赖未启动或凭据缺失必须标为失败/阻塞，不能假绿。

#### 从哪里开始

先确认仓库实际使用的 CI 平台/runner/凭据，不从项目名推断一定是 GitHub。已有执行入口是 [Makefile](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/Makefile)、[rag-test/main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/rag-test/main.go)和 [Thresholds.Check](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/eval/thresholds.go)；流水线文件按确认的平台新增，本计划不假称已有。

#### 按顺序做

1. 先接纯 Go 测试与契约生成检查；确保日志可下载，生成检查不改用户工作区。
2. 准备专用评测依赖、语料和密钥；明确哪些变更触发真实服务评测、哪些运行需要审批与费用预算。
3. 运行 P20/P21，归档报告与版本信息；P22 确定性的检查自动跑，人工项设签核，不伪装全自动。
4. 报告比较先判配置/语料/模型/通道/粒度兼容，再对比整体和逐场景指标。
5. 故意构造失败用例验证非 0 退出确实阻断发布；之后恢复并证明确实放行。

**用户验收产物：** 一次绿色流水线、一次真实红色阻断、对应提交与可下载报告；缺服务/跳过真实评测时状态明确，不静默绿色。

### P24｜维护清理接线与 dry-run

**状态：本地契约与隔离测试已完成；真实数据库与多实例演练待验证。**

- [x] 已通过独立 [cmd/maintenance](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/maintenance/main.go) 接入维护入口，支持 worker 配置中的周期、保留期、批大小、SIGINT/SIGTERM 优雅退出和 `-periodic`。
- [x] 已提供 `-dry-run` 只读预览、逐类删除/过期计数和隔离测试样例；预览不会调用写入接口，并保留未终态与未过期记录。
- [ ] 真实 PostgreSQL 中的历史数据、副本恢复、异常中断续跑和多实例协调仍需隔离环境验证。
- **用户验证：** 先运行本地 P24；真实环境再导入已过期/未过期/运行中样例，dry-run 后确认无写入，再执行清理并重跑。
- **通过标准：** 本地只清理满足规则的记录；预览与实际计数可核对；真实演练还需证明重复执行不多删。
- **异常验证：** 多实例不会造成冲突或重复破坏；异常中断可恢复，禁止对生产数据做演练。

#### 从哪里开始

维护入口是 [cmd/maintenance](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/maintenance/main.go)，核心实现是 [maintenance.Worker.Preview/RunOnce/Run](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/maintenance/worker.go)。清理接口分别在 [approval store](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/persistence/approval/approval.go)、[checkpoint store](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/persistence/checkpoint/store.go)、[turn store](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/persistence/turn/store.go)；配置定义看 [MaintenanceConfig](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/config/config.go)。

#### 按顺序做

1. 先决定由独立维护入口还是某一进程启动；说明多实例策略，不直接给每个副本启动一个不协调的循环。
2. 增加只查询的候选预览/计数路径，再执行实际更新/删除；不能用“执行后回滚事务”冒充无副作用预览。
3. 拆清“过期 pending 审批改状态”与“删除旧终态记录”，分别列策略和时间基准。
4. 接周期、批量大小、保留期、停止信号和错误日志；间隔非法在启动时拒绝。
5. 固定过期/未过期/执行中三类记录，预览→执行→重跑，对照计数；演练中断后继续。

**用户验收产物：** 候选清单、实际计数、保留记录与下次执行时间。只完成 `go Worker.Run()` 接线而没有删除边界，不算交付。

### P25｜业务指标与分级健康信息

**状态：本地指标契约与健康状态测试已完成；真实抓取、故障和告警演示待验证。**

- [x] 已增加受控标签的索引成功/失败/重试、队列积压、检索耗时、检索结果、降级和健康状态指标；不使用 document ID、用户、问题正文或错误全文作为标签。
- [x] 已约定组件健康状态的状态切换语义，并将业务指标注册到 go-zero 使用的 Prometheus 默认 registry；采集端不可用不阻塞业务代码。
- [ ] 真实 Prometheus 抓取、可选依赖故障、告警阈值和 API/worker 健康矩阵仍需隔离环境验证。
- **用户验证：** 先运行本地 P25；真实环境再上传、搜索、制造一次可恢复索引失败，抓取指标，停用一个依赖查看健康与告警。
- **通过标准：** 本地指标随调用变化且标签低基数；真实环境还需证明失败不会报全健康，可选 ES 缺失不错误等同进程死亡。
- **异常验证：** 指标不暴露密钥/正文，采集服务不可用不拖死业务。

#### 从哪里开始

基础指标/日志在 [observability](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/observability/metrics.go)和 [REST 启动](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/restapi.go)；业务观测点在 [Indexer](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/indexer.go)与 [RetrieveDetailed](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/retriever.go)；就绪探测看 [ReadyLogic.Ready](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/health/ready_logic.go)，单元与本地契约入口为 `bash scripts/acceptance/run.sh P25`。

#### 按顺序做

1. 先列最少指标：索引成功/失败、任务积压、检索延迟与降级；再列健康依赖分类，不先做大屏。
2. 在明确阶段边界计数，任务重试数与最终文档失败数分开，避免每次重试重复算成最终失败。
3. 指标标签只用受控枚举，不用任意 document ID、用户、问题文本或错误全文做高基数标签。
4. 明确 HTTP 和 worker 的就绪要求不同；可选 ES 降级和数据库失联分开，不把所有依赖绑定 liveness 造成重启循环。
5. 做正常操作前后抓取、依赖故障和一条告警测试；告警阈值需以基线确认。

**用户验收产物：** 指标名/含义/标签清单、操作前后抓取片段与健康故障矩阵；已有原生 HTTP 指标保留，不重复建设私有替代。

### P26｜跨 HTTP、队列与 worker 的完整追踪

**状态：本地 trace envelope、跨进程关联和脱敏边界已完成；真实 collector 链路待验证。**

- [x] 已在队列适配层传递/关联 trace 上下文和任务类型，覆盖投递、消费、重试关联；业务 payload 不需要增加 trace 字段。
- [x] 已提供本地 OTLP collector 配置、trace envelope 回归测试和旧裸 payload 兼容边界；追踪属性使用受控字段，不写入密钥或完整正文。
- [ ] 真实 HTTP→Redis/worker→embedding/index 写入的 collector 查询、重试可视化和 collector 中断演练仍需隔离环境验证。
- **用户验证：** 先运行本地 P26；真实环境再上传一份文档，在追踪系统按 trace/任务 ID 查看从 HTTP 受理到 worker ready 的完整链路。
- **通过标准：** 本地跨进程传输合同通过；真实环境还需证明跨度与耗时不只覆盖 HTTP 请求。
- **异常验证：** collector 中断业务仍可运行；追踪中无密钥或完整私有正文。

#### 从哪里开始

队列透传实现见 [asynq/trace.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/queue/asynq/trace.go) 的 `encodeEnvelope/decodeEnvelope/startPublishSpan/startProcessSpan`；producer/consumer 调用点看 [asynq/client.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/queue/asynq/client.go)；trace 初始化看 [observability/trace.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/observability/trace.go)，本地 collector 配置看 [otel-collector.yaml](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/configs/observability/otel-collector.yaml)。

#### 按顺序做

1. 先配置隔离 collector 和查看工具，让 HTTP 与 worker 均导出到同一后端；无 collector 就不能验收真实链路。
2. 以 P04 上传产生一个任务，记录 HTTP trace ID 和 document/task ID，查 producer/consumer 是否相连。
3. 缺哪段补哪段，优先检查 context 是否丢失；不要把 trace 字段直接加到业务 payload，现有信封已负责传输。
4. 验证重试是同一链路中的可识别消费尝试；旧裸 payload 兼容仍能消费。
5. 停 collector 后做一次业务操作与正常退出，确认导出不会无限阻塞。

**用户验收产物：** 可查询的 trace ID、完整链路截图/导出、重试样例和故障结果。只有日志里有 trace ID，不等于跨进程链路已通过。

### P27｜版本化数据库迁移

- [x] 已交付显式版本化迁移、`schema_migrations` 状态查询、dry-run 和 `cmd/db-migrate`；开发便利入口与生产安全连接分离。
- [x] API/worker/导入/维护等应用启动使用无迁移连接；生产路径不启用删列/删索引选项。
- [ ] 真实空库、历史副本、备份恢复和失败发布阻断待隔离环境验证。
- **用户验证：** 先执行 `bash scripts/acceptance/run.sh P27`；真实环境再在含历史样例的隔离旧版本库升级，核对迁移版本、行数及主链路，重跑并执行恢复演练。
- **通过标准：** 本地安全启动边界通过；真实环境还需证明服务重启不擅自删列/索引，历史数据保留，失败可恢复到可服务状态。
- **异常验证：** 版本不兼容和中途失败明确终止，不能继续对不确定结构提供业务。

#### 从哪里开始

最关键入口是 [db-migrate](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/db-migrate/main.go) 与 [entx migration](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/storage/entx/migration.go)：迁移由显式命令执行，应用连接使用 `OpenWithoutMigration`，不在启动时执行 `Schema.Create` 或昵称回填。Schema 定义看 [user.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/ent/schema/user.go)、[document.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/ent/schema/document.go)等；先处理这个边界，不先做索引切换。

#### 按顺序做

1. 明确生产和开发模式的迁移约定，列现有库版本基线；在库副本生成迁移，不对原库直接试。
2. 把生产连接与迁移分开：HTTP/worker/rag-test 不自动改结构；需要迁移时启动前明确检查并拒绝不兼容版本。
3. 选择可维护的版本化迁移工具/入口，交付迁移文件和版本记录；具体安装/版本另经确认，不在本次计划中擅自安装。
4. 将昵称等数据回填纳入有版本、可审计的数据迁移，避免每次连接都悄悄改数据。
5. 对含历史数据的旧库副本升级：校验行数/字段/账号/文档主链路，再重跑，验证幂等。
6. 先备份并演练恢复，标记不可逆 DDL；不能声称每个结构变化都能无损 down。

**用户验收产物：** 迁移前后版本、SQL/迁移文件、历史数据校验、启动兼容检查和实际恢复证据。生产发布前必需，不等待 P16/P19 等可选项。

### P28｜影子索引重建，不影响旧版本

- [x] 已交付本地版本元数据、building/ready/failed 状态、进度和活动指针控制平面。
- [x] 已交付版本 create/progress/complete/fail/status 与显式切换前门禁；默认活动版本不会自动改变。
- [ ] 真实 ES/Milvus 独立索引重建、完整性校验和逐版本评测待隔离环境。
- **用户验证：** 先执行 `bash scripts/acceptance/run.sh P28`；真实环境再旧版持续查询时重建新版，对照进度和隔离结果，并验证中断恢复。
- **通过标准：** 本地控制平面通过；真实环境还需证明旧版仍可服务、新版未完成前不成为活动版，报告能对应到同一版本。
- **异常验证：** 重建失败不破坏旧版；模型维度不匹配不能混合写入或查询。

#### 从哪里开始

当前 worker 索引写入见 [worker/main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/worker/main.go)与 [Indexer](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/indexer.go)；向量集合见 [MilvusStore](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/store/milvus/milvus.go)，关键词索引见 [ES 包](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/storage/es/search.go)及 [mapping](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/configs/es/chunk_mapping.yaml)。现有配置里有 collection/index 名称，不等于已有版本化重建能力。

#### 按顺序做

1. 先定义不可变版本元数据：语料快照/水位、embedding 模型和维度、mapping/解析版本、collection/index 对；不只起一个新名字。
2. 定义 `building/ready/failed/active` 等明确状态及完成判据，活动指针与构建版本分开。
3. 设计正文/分块变更期间的策略：冻结验收语料，或实现快照+增量追赶；不确认一致性就无法宣称重建完成。
4. 为新版写入独立的向量/关键词索引，记录构建进度和失败点，旧查询继续指向旧版。
5. 对新版用对应版本配置跑 P20，检查完整性、维度、mapping 和质量后才允许进入可切换状态。
6. 中断构建恢复重跑，核对不会污染旧版本；构建数据清理另有安全台账。

**用户验收产物：** 同一时段旧版持续查询、新版构建进度、版本清单、新版完整性/质量报告。不要只手改全局 `Milvus.Collection` 同时把旧服务切走。

### P29｜索引切换、回滚与故障恢复

- [x] 已交付本地显式 switch/rollback 入口；只有 ready 版本可切换，失败版和 active 版不能被错误操作。
- [x] 已覆盖状态重载、重复 status、旧版保留和失败状态门禁；真实请求级/多实例一致性待接线。
- [ ] 真实环境演练重建失败、切换失败、切换后回滚和多实例状态确认。
- **用户验证：** 先执行 `bash scripts/acceptance/run.sh P29`；真实环境再切换合格新版→查固定问题→回滚旧版→尝试切换未完成版本。
- **通过标准：** 本地安全契约通过；真实环境还需证明合格版可服务、旧版可回滚、失败保留最后有效版且不会混用新旧配置。
- **异常验证：** 未完成/不合格版本拒绝切换；回滚依据不随切换自动删除。

#### 从哪里开始

从 P28 的版本模型/活动指针着手，实际读入口是 [newRetrievalSearcher](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/restapi/retrieval.go)和 [rag-test/main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/rag-test/main.go)，知识工具也必须使用一致的活动版本。当前静态配置句柄不自动获得在线原子切换能力，需要明确设计。

#### 按顺序做

1. 定切换入口与权限，确认是受控重启切换还是在线句柄替换；不假定多实例自然同时更新。
2. 切换前检查新版本 ready、完整性、评测门禁和兼容性；锁定一个请求的向量/关键词/正文版本组合。
3. 记录活动版本、切换人、旧版、时间和各实例确认结果；超时/部分实例失败时有明确回退策略。
4. 执行旧→新→旧三轮固定问题，核对返回证据与活动版本，而不只是分数变化。
5. 对未完成版本、门禁失败版本和单实例更新失败分别演练；不删除旧索引来省空间直到保留窗口届满并确认。

**用户验收产物：** 切换/回滚命令或 API、前后版本报告、多实例一致性和故障恢复证据；恢复成功后再单独讨论旧索引清理。

## 五、阶段出口：完成一批，用户就能确认一批

| 阶段 | 必须满足的交付条件 | 不能据此宣称什么 |
| --- | --- | --- |
| 主链路可联调 | P01–P09、P12、P20 完成对应验收，上传→ready→召回→引用有证据 | 不能宣称已经生产可发布 |
| 质量可持续 | P21–P23 已验收，真实基线和 CI 阻断可复现 | 不能把固定阈值当成所有业务的永久保证 |
| 增强能力可用 | 选定的 P10–P19 每项独立验收，条件性项单列状态 | 未选择或阻塞项不能自动标为完成 |
| 生产运行可恢复 | P24–P29 验收，迁移、观测、清理、切换与恢复证据齐全 | 不替代容量测试、部署安全和运维确认 |

## 六、用户验收记录模板

| 计划编号 | 交付版本 | 开发自测 | 用户复测 | 报告/证据 | 最终状态与问题 |
| --- | --- | --- | --- | --- | --- |
| P__ | commit + 工作区摘要 | 通过/失败/阻塞 | 通过/不通过/未复测 | 本轮报告目录 | 待验收/已验收/需返工 |

每完成一项，开发者给用户：**改了什么、执行什么、应该看到什么、报告在哪里、失败如何恢复**。用户复测确认后再将该项标为“用户验收通过”，无需等 29 项全部做完。

## 七、暂不混入本轮的需求

数据集编辑/状态切换、用户完整生命周期、独立前端页面、PDF/Office 摄取和依赖全面升级，不因“生产化”名义自动扩范围。若决定补做，应各自新增编号与验收卡；不覆盖上述计划的完成标准。

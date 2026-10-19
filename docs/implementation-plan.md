# Eino Harness 分项实施与用户验收计划

> 编制日期：2026-10-06（Asia/Shanghai）  
> 依据：[当前开发进度](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/docs/current-progress.md)；本次补充核对时 HEAD 为 `6dacfbd`（2026-10-06，「完成部分」）。  
> 范围：当前后端仓库。独立前端页面不作为本计划完成的默认前提。  
> 本文记录分项计划与验收卡。P01-P09、P12、P20 已完成本地可复现验收；真实 PostgreSQL/Redis/worker/模型/向量与搜索依赖仍按各项边界单独复测。

## 1. 执行原则：每完成一项，用户就能验收一项

本计划拆成 **29 个可独立交付的工作包**。已有功能优先补验证和缺陷，不从头重做；新增能力交付到真实 API/工具/运行入口，不以“接口定义好了”作为完成。

每项提交必须具备：

1. **可执行交付物**：业务实现/修复，以及最小可复现样例。
2. **验收入口**：已有测试命令，或随该项交付的编号验收脚本。
3. **用户可见证据**：真实 API 响应、文件变化、索引状态、指标或报告；不能只有开发者口头说明。
4. **失败边界**：至少一个坏输入或故障用例，清楚说明预期行为。
5. **运行说明**：前置配置、启动命令、测试凭证来源、预计等待上限、样例清理方法。
6. **完成门槛**：对应测试通过、样例可复测、业务效果符合验收标准，并由用户确认。

说明：

- 测试数据必须使用合成语料和隔离数据集/数据库，不在真实业务数据上做故障或清理演练。
- 真实模型验证与 mock 验证分别标记；mock 只能证明协议和控制流，不能证明真实检索/回答质量。
- 本计划不预设日期或承诺工期；按依赖顺序推进，避免未确认的外部条件阻塞基础链路。
- API 可以使用 curl、Postman 或已有管理页面验证；不要求为了后端验收先新建 UI。

## 2. 统一验收方式

### 2.1 已存在的原生命令

以下命令现在已有对应入口；P01 的全量回归已于 2026-10-07 通过，真实检索仍需要完整测试依赖与匹配语料：

```bash
export REPO="/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2"
cd "$REPO"

# P01：避免只看缓存，直接重新执行全量测试
# 需要允许监听本机测试端口的普通本地终端。
go test ./... -count=1

# P20：真实召回评测；-config 必须指向隔离的验收环境。
# 输出路径、数据集及数据集类型由 P20 提供的样例说明指定。
go run ./cmd/rag-test \
  -config /absolute/path/to/acceptance-config.yaml \
  -dataset "$REPO/internal/eval/datasets/retrieval.jsonl" \
  -thresholds "$REPO/internal/eval/thresholds.yaml" \
  -out "$REPO/logs/acceptance/eval-report.json" -v
```

上面的 `/absolute/path/to/acceptance-config.yaml` 是需要替换的示例路径；当前语料若与现有金标不匹配，应先完成 P20 的固定语料对齐，不能直接把评测失败解释为代码缺陷。

### 2.2 计划交付的编号验收入口

**编号脚本已交付：P01-P29 的本地验收入口均可执行；本地契约用例已通过，真实模式仍会明确区分 BLOCKED/待复测。每一项只有在对应实现和用例交付后才可执行。**

四个批次也提供聚合入口，见 [四个交付批次与直接验收入口](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/docs/delivery-batches.md)：`bash scripts/acceptance/run-batch.sh 1 --local` 到 `4 --local`。

```bash
export REPO="/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2"

# 列出真正已经交付的验收项
bash "$REPO/scripts/acceptance/run.sh" --list

# 示例：完成 P04 后，用户只需执行这一条
bash "$REPO/scripts/acceptance/run.sh" P04

# 示例：完成 P10 后，直接验收文档启停
bash "$REPO/scripts/acceptance/run.sh" P10
```

入口要求：

- 接收测试配置和已有合法测试凭证；脚本不得硬编码或输出密钥。
- 每项自动创建自己的测试资源，记录 ID 和实际请求/响应；轮询有超时，不要求用户手查数据库。
- 输出 `PASS` / `FAIL` / `BLOCKED`；只有所有本项断言通过才退出 0。依赖缺失和未实现也必须非零退出。
- 报告位于 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/<run-id>/`，包含摘要、脱敏响应、前后对比、版本及配置指纹。
- 区分“控制流验收通过”与“真实模型质量达标”；脚本不能用宽松字符串判断或只看 HTTP 200 判成功。
- 不自动停止已有服务，不覆盖已有配置，不默认清理历史测试数据；需要清理时列出范围并显式确认。

### 2.3 每项报告必须让用户看到什么

| 报告内容 | 用户如何判断 |
| --- | --- |
| 计划编号、代码版本、运行模式 | 确认验证的是这次交付，不是旧结果或 mock 冒充真实运行 |
| 输入语料、请求、预期结果 | 不读业务代码也能理解验收场景 |
| 实际输出、断言逐项结果 | 能对照“预期 vs 实际”，而不是只看到 PASS |
| 异常/故障场景 | 能证明失败不会被静默掩盖 |
| 测试资源 ID、保留/清理方式 | 能用 API 或管理页面自行复查 |
| 不通过时的定位及重试方式 | 用户知道缺配置、缺功能还是实际质量不达标 |

## 3. 推进顺序与状态总表

当前计划按“本地契约已通过 / 真实依赖待复测 / 待执行”区分。表中的“类型”区分已经存在代码的验收工作与真正新增开发；本地通过不等于外部依赖端到端已通过。

- **阶段 A：先把已有链路验收到可复现。** 先 P01、P02；P04 后可分别做 P09、P12、P06；P05 依赖上传验收 P12；P07、P08 在真实模型与绑定可用后做。
- **阶段 B：逐项补齐功能。** P10–P13 可先做；P14–P17 要在 P20 基线之后做，才能量化改善；P18 需要对话联调；P19 取决于真实上游条件。
- **阶段 C：形成质量门禁。** P20 可以在阶段 A 后立即做，不必等所有增强功能；然后做 P21/P22，最后 P23。
- **阶段 D：生产化。** P24–P27 依赖满足后可分别推进；P28/P29 在迁移与基线完善后做。

**建议第一个交付批次：P01 → P02 → P04 → P12 → P05 → P06/P09 → P07 → P08 → P20。** 完成后用户可以验证已有主链路，而不是只看到配置或单测。

LLM 自动拆分、真实用户上游同步属于条件性增强；不必把它们作为现有结构化型录和基本问答链路首次交付的前置条件。

| 编号 | 计划 | 类型 | 前置计划 | 用户直接看到的结果 |
| --- | --- | --- | --- | --- |
| P01 | 修复指标配置，让全量测试全绿 | 收敛现有实现 | 无 | 全量测试全绿、指标可抓取 |
| P02 | 交付隔离的联调环境和统一验收入口 | 新增验证基础设施 | P01 | 依赖预检和编号验收报告 |
| P03 | 统一运行文档、API 契约和生成物 | 修正文档与契约 | P02 | 文档步骤可复现、契约无漂移 |
| P04 | 验收文档创建、索引、更新、重建、删除闭环 | 验证已有业务链路 | P02 | 文档 ready、正文可读、更新/删除生效 |
| P05 | 验收产品拆分和重复上传增量更新 | 验证已有业务链路 | P04、P12 | 一产品一文档、重复上传不重复 |
| P06 | 验收三通道召回、归并与降级提示 | 验证已有业务链路 | P04 | 三通道结果、归并单位与降级提示 |
| P07 | 验收多 Agent 对话、SSE 和历史保存 | 验证已有业务链路 | P06、P09 | 流式回答、可追溯引用、历史消息 |
| P08 | 验收审批列表、通过/拒绝与检查点续跑 | 验证已有业务链路 | P07 | 审批前不写入，通过后只执行一次 |
| P09 | 验收主体矩阵和知识库绑定的即时生效 | 验证已有管理接口 | P04 | 矩阵两侧一致、绑定变更生效 |
| P10 | 把文档启停改为可重试的明确状态操作 | 完善现有接口 | P06 | 停用不召回，重复请求不翻转 |
| P11 | 补无 ES 时的结构化精确检索 | 补齐已有缺口 | P06 | 无 ES 仍有真正型号精确命中 |
| P12 | 完善多文件上传、大小限制与失败提示 | 完善已有上传 | P04 | 多文件可用、超限和部分失败清楚 |
| P13 | 交付目录批量导入和增量重跑 | 新增导入功能 | P12、P05 | 目录导入、dry-run、增量重跑 |
| P14 | 接入真实 Rerank，并能查看排序前后差异 | 新增检索能力 | P20 | 排序前后差异和真实服务评测 |
| P15 | 补 Rerank 候选预算、超时与降级 | 完善新增能力 | P14 | 候选受限，超时后有可解释降级 |
| P16 | 接入 LLM 产品拆分，支持无 front-matter 原文 | 可选增强 | P05、P20 | 无 YAML 原文拆成正确产品文档 |
| P17 | 确保 LLM 拆分可重放和并发重传不重复 | 完善可选增强 | P16 | 重传/并发/重启不增生文档 |
| P18 | 让指定默认 Agent 真正使用 Skill | 补齐已有接线 | P07 | 真实加载 Skill 的工具记录 |
| P19 | 接入外部用户同步，而非只保留 501 | 本地契约已完成；真实上游待条件确认 | P02、P09 | 本地新增/更新/跳过/失败台账；真实上游仍待验收 |
| P20 | 交付真实召回基线和一键复测 | 验证已有评测能力 | P06、P05 | 真实指标、逐用例报告和复测结果 |
| P21 | 补私有语料的可见性隔离评测 | 补齐评测盲区 | P09、P20 | 非空私有用例、资料隔离结果 |
| P22 | 补知识问答无答案与引用正确性验收 | 新增回答质量验收 | P07、P20 | 无答案不编造、引用有证据 |
| P23 | 交付 CI 质量门禁和跨版本报告对比 | 新增持续验证 | P01、P03、P20、P21、P22 | 流水线放行/阻断及可下载对比报告 |
| P24 | 启动维护清理任务并交付 dry-run | 补齐已有接线 | P08 | dry-run 清单和实际清理计数 |
| P25 | 交付索引/检索业务指标与分级健康信息 | 新增运行可观测性 | P01、P04、P06 | 业务指标随操作变化、健康分级 |
| P26 | 验收 HTTP→队列→worker 的完整追踪 | 完善追踪闭环 | P04、P25 | 同一 trace 中看到 HTTP 与 worker |
| P27 | 用版本化迁移替代生产启动自动改表 | 生产化必需项 | P04 | 迁移版本、历史数据保留、恢复结果 |
| P28 | 交付影子索引重建，旧索引仍可服务 | 新增索引版本能力 | P20、P25、P27 | 新旧索引隔离，旧版查询不断 |
| P29 | 交付索引切换、回滚与故障恢复演练 | 完善索引版本能力 | P28、P23 | 切换/回滚前后版本及故障演练结果 |

## 4. 分项实施与验收卡

每项都必须交付下列验收步骤对应的真实证据；编号脚本由该项同步补齐，不得只新增一个打印 PASS 的空脚本。

### P01｜修复指标配置，让全量测试全绿

**阶段 / 类型：** A / 收敛现有实现　　**前置：** 无　　**状态：** 已完成（2026-10-07）

**拆分交付：**
- [x] 明确默认部署显式启用指标；默认绑定 `127.0.0.1:9091/metrics`，与业务端口分离。
- [x] 修复测试初始化中的指标路径和端口配置，测试使用独立随机临时端口，不占用真实服务端口。
- [x] 补启用/禁用两种配置测试；真实 middleware 请求后抓取 go-zero agent 指标。
- [x] 全量 `go test -count=1 ./...` 退出码为 0，并保存原始输出。
- [x] 提供 `scripts/acceptance/run.sh P01` 和 `P01-live` 作为可重复验收入口。

**用户直接验证：**
1. 在普通本地终端执行下方全量测试命令，确认退出码。
2. 按交付的开发配置启动 HTTP，访问指标地址，并发一次健康请求后再次抓取指标。

**验收入口：** 在仓库根目录执行 `go test ./... -count=1`，并按交付说明抓取本地指标；不依赖尚未实现的 P02 脚本。

**通过标准：** 全量测试退出码为 0；启用时能抓取 go-zero HTTP 请求指标；关闭时行为与文档一致。

**失败/异常必须验证：** 禁用配置不应意外启动指标端口；测试因端口权限受限时明确报告环境错误，不能记成通过。

**验收说明：** [P01 指标验收指南](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/docs/p01-metrics-acceptance.md)。

**当前源码：** [chain_test.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/chain_test.go)。

### P02｜交付隔离的联调环境和统一验收入口

**阶段 / 类型：** A / 新增验证基础设施　　**前置：** P01　　**状态：** 本地工具完成；真实依赖预检待环境（2026-10-07）

**拆分交付：**
- [x] 提供本地配置示例、依赖清单、启动/停止说明及只读/短 TTL 依赖预检；与现有业务数据隔离。
- [x] 提供可选的 loopback-only Docker 依赖栈和受控脚本；仅管理 `eino-acceptance` compose 项目，默认停止时保留 named volume。
- [x] 交付 `scripts/acceptance/run.sh`，支持 `--list`、P02 `--degraded/--complete`、编号执行和脱敏 JSON 报告。
- [x] 交付带唯一运行标记的 fixture；清理必须显式传入指定 P02 报告目录和 `--confirm`，只删除本轮 fixture。
- [x] 预检区分完整检索配置与允许降级配置；Redis 可连接但没有 worker 注册时明确 BLOCKED，不能伪装成写入可用。

**用户直接验证：**
1. 填写交付说明中要求的本地配置和已有合法测试凭证，启动独立 HTTP 与 worker。
2. 执行 P02 验收；访问 /health 和 /ready，查看预检列出的各依赖结果。
3. 临时使用一个不存在的测试依赖地址重跑预检，随后恢复原配置。

**验收入口：** `bash "$REPO/scripts/acceptance/run.sh" P02 --degraded` 或 `P02 --complete`；报告目录内保存 `preflight.json`、`summary.json`、`preflight.log` 和唯一 fixture，用户可按输出路径直接复查。

**通过标准：** 完整配置下预检通过，两个进程运行；错误配置明确指出缺失依赖；报告记录配置摘要但不含密钥。

**失败/异常必须验证：** 端口冲突、凭证缺失或服务不可用时非零退出并给出修复提示；不得修改现有 .env 或停止用户已有服务。

**当前实现：** [acceptance-preflight/main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/acceptance-preflight/main.go)、[run.sh](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/scripts/acceptance/run.sh)、[cleanup-p02.sh](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/scripts/acceptance/cleanup-p02.sh)、[dependencies.sh](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/scripts/acceptance/dependencies.sh)、[compose.yaml](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/configs/acceptance/compose.yaml)。

### P03｜统一运行文档、API 契约和生成物

**阶段 / 类型：** A / 修正文档与契约　　**前置：** P02　　**状态：** 已完成本地契约验收；真实环境复测待补

**拆分交付：**
- [x] 修正上传、正文存储、产品拆分阶段、chunk 统计、默认端口等历史说明。
- [x] 核对 .api、Go 类型/路由、Swagger 与 TypeScript 契约；保留手写业务逻辑及请求适配层。
- [x] 新增临时目录生成/比对检查，报告纯生成物差异，不直接覆盖用户已有改动。

**用户直接验证：**
1. 只按更新后的快速开始说明启动服务，不依赖口头补充步骤。
2. 执行 P03 检查，并使用 Swagger/契约确认上传和召回的请求、返回字段。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P03`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 文档中的地址、请求字段和异步状态说明能直接复现；生成物与声明一致；检查报告无未解释差异。

**失败/异常必须验证：** 在隔离副本中删掉一个生成字段后检查应失败；缺少生成工具时应报告前置条件，不能假报一致。

**本地验收记录（2026-10-07）：** `bash scripts/acceptance/run.sh P03` 与 `bash scripts/check-api.sh` 已通过；真实服务启动复测仍依赖 P02 环境。

**当前源码起点：** [restapi.api](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/docs/restapi.api)。

### P04｜验收文档创建、索引、更新、重建、删除闭环

**阶段 / 类型：** A / 验证已有业务链路　　**前置：** P02　　**状态：** 已完成本地契约验收；真实依赖联调待复测

**拆分交付：**
- [x] 新增普通 Markdown 固定语料和 API 集成验收；返回动态创建的 datasetId/docId。
- [x] 覆盖 JSON 创建、后台索引、正文读取、修改正文、单文档重建、整库重建及删除。
- [x] 轮询具有明确超时；报告保存状态变化和分块统计，不要求一定能捕获短暂的 indexing 状态。

**用户直接验证：**
1. 创建测试数据集和文档，等待 ready；读取正文，搜索固定标记词。
2. 修改正文中的标记词并等待 ready，确认新词可以召回；分别执行两种重建。
3. 删除测试文档，重新请求详情和搜索。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P04`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 正文与提交内容一致；索引完成后分块统计正确；重建不产生重复文档；删除后不再召回。

**失败/异常必须验证：** 使用隔离配置让索引依赖不可达：请求或任务状态明确失败，不能无限显示处理中；恢复后可重建成功。

**本地验收记录（2026-10-07）：** `bash scripts/acceptance/run.sh P04` 已通过；报告覆盖真实 Service/Indexer/Ent 内存库和 HTTP handler 契约，PostgreSQL/Redis/Milvus/ES 实联仍待复测。

**当前源码起点：** [service.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/service.go)。

### P05｜验收产品拆分和重复上传增量更新

**阶段 / 类型：** A / 验证已有业务链路　　**前置：** P04、P12　　**状态：** 已完成本地契约验收；真实依赖联调待复测

**拆分交付：**
- [x] 准备三个不同型号的产品型录，保证正文和 front-matter 元数据可核对。
- [x] 覆盖首次导入、相同文件重传、只改一个产品、重复型号四种情形。
- [x] 保存文档 ID、operation 与索引进度，补错误型录不被静默接受的用例。

**本次实现（2026-10-07）：** 新增 `TestProductCatalogIncrementalUpsert`，使用真实 Service、Ent 内存库、Markdown pipeline、Indexer 和受控队列；验证一产品一文档、external_key 稳定、原样重传 unchanged、单产品 updated、元数据单独更新、失败后重试、重复型号/坏 YAML/未闭合 YAML 拒绝及普通文档回落。

**本地验收记录：** `bash scripts/acceptance/run.sh P05` 已通过，最近报告目录为 `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/P05/20261007-103825-43717-local`。

**用户直接验证：**
1. 上传两个产品的型录，确认返回两条产品文档；等到 ready 后重传相同文件。
2. 只修改一个产品并重传，再仅修改它的系列元数据并重传。
3. 提交重复型号、坏 YAML 或未闭合 front-matter 的测试文件。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P05`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 首次 created；不变产品 ID 保持且 operation 为 unchanged；变化产品 updated，不新建重复型号；正文不包含 YAML 头。

**失败/异常必须验证：** 格式错误有明确错误；产品仅改元数据也必须被识别为变化；不能忽略后一个同型号产品。

**当前源码起点：** [split.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/split.go)。

### P06｜验收三通道召回、归并与降级提示

**阶段 / 类型：** A / 验证已有业务链路　　**前置：** P04　　**状态：** 已完成本地契约验收；真实依赖联调待复测

**拆分交付：**
- [x] 准备受控产品/普通文档检索样例，覆盖型号、元数据词和语义查询。
- [x] 验证 top_k、granularity、matched_chunks、chunk_budget、channels、degraded 与截断标记。
- [x] 通过受控 keyword/vector/exact 适配器覆盖完整与降级路径；真实完整/无 ES 配置联调待复测。

**本地验收记录（2026-10-07）：** `bash scripts/acceptance/run.sh P06` 已通过，覆盖 exact/keyword/vector 的成功/失败降级、精确匹配、document/chunk 归并、粒度、召回预算和正文截断元数据。真实 PostgreSQL/Milvus/ES 结果仍待复测。
**用户直接验证：**
1. 在完整配置下执行型号、关键词、语义搜索，检查命中文档及通道台账。
2. 分别搜索产品库和普通库，比较结果的 document/chunk 粒度及条数。
3. 切换无 ES 配置再次检索仅存在于元数据中的型号，查看降级信息。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P06`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 命中符合固定语料；结果条数不超过服务端上限；归并单位正确；无 ES 回落仍能检索元数据并明确标记 exact 降级。

**失败/异常必须验证：** 一个通道异常不会被伪装成正常空结果；全部依赖不可用时明确报错；无相关结果与依赖失败可区分。

**当前源码起点：** [search.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/search.go)。
### P07｜验收多 Agent 对话、SSE 和历史保存

**阶段 / 类型：** A / 验证已有业务链路　　**前置：** P06、P09　　**状态：** 已完成本地协议验收；真实模型/依赖联调待复测

**拆分交付：**
- [x] 提供确定性 HTTP/SSE 协议测试，覆盖知识问答入口所需的请求校验和帧格式。
- [x] 固定 `message/done/error` 等帧格式断言，避免客户端依赖未声明的 HTTP event 行。
- [ ] 真实模型联调样例、会话历史、引用、断流与只读 workspace 证据待 P02 环境复测。

**本地验收记录（2026-10-07）：** `bash scripts/acceptance/run.sh P07` 已通过，覆盖路由可达、请求校验和生成格式 SSE 帧；真实模型回答、历史落库、断流和引用链路仍待复测。
**用户直接验证：**
1. 对已绑定的测试知识库提问，看到流式回答和可以定位原文的引用。
2. 要求读取验收工作区内固定文本文件，确认返回实际内容，文件未被修改。
3. 连续追问，再用历史消息接口查询；测试中断一个流请求。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P07`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 知识回答对应测试语料，引用可追溯；只读任务没有写文件；历史包含实际完成的消息；断流不继续无意义执行。

**失败/异常必须验证：** 空输入或模型请求失败产生明确 error；mock 协议测试通过不能代替真实模型回答验收。

**当前源码起点：** [chat_logic.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/stream/chat_logic.go)。
### P08｜验收审批列表、通过/拒绝与检查点续跑

**阶段 / 类型：** A / 验证已有业务链路　　**前置：** P07　　**状态：** 已完成本地状态机验收；真实 HTTP/Agent 联调待复测

**拆分交付：**
- [x] 以 Ent SQLite 验证审批记录、检查点、批准、Claim/Consume 单次保护和审计脱敏。
- [x] 覆盖已执行审批的重复恢复失败边界，确认不会重复消费。
- [ ] HTTP/Agent 的隔离 workspace 写入、拒绝、过期、进程重启后恢复和异常退出证据待真实环境复测。

**本地验收记录（2026-10-07）：** `bash scripts/acceptance/run.sh P08` 已通过，使用 Ent SQLite 验证创建、检查点、批准、Claim/Consume 单次保护和审计脱敏；真实 HTTP/Agent 审批联调仍待复测。
**用户直接验证：**
1. 请求写入测试文件；收到 approval_required 后确认文件尚不存在，查看审批列表和详情。
2. 批准并恢复，确认文件内容正确；重复恢复同一审批，确认未重复执行。
3. 新建第二个写入请求并拒绝，确认文件不生成；另建请求后重启 HTTP 再恢复。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P08`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 审批前不执行，批准后按原操作执行一次，拒绝不执行；状态变化和会话结果可核对，检查点可跨重启恢复。

**失败/异常必须验证：** 过期或已消费审批不重复执行；恢复失败有清晰结果和可重试边界，不留下永久“恢复中”。

**当前源码起点：** [resume_approval_logic.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/stream/resume_approval_logic.go)。
### P09｜验收主体矩阵和知识库绑定的即时生效

**阶段 / 类型：** A / 验证已有管理接口　　**前置：** P04　　**状态：** 已完成本地绑定即时生效验收；真实身份矩阵联调待复测

**拆分交付：**
- [x] 在知识工具本地测试中覆盖主体绑定动态解析，不缓存上一次调用的授权结果。
- [x] 验证绑定变更在下一次调用即时生效，并覆盖无授权主体的空结果边界。
- [ ] 真实主体列表、筛选、分页、正反向绑定查询及双身份 API/对话证据待复测。

**本地验收记录（2026-10-07）：** `bash scripts/acceptance/run.sh P09` 已通过，验证知识工具每次调用重新解析当前主体绑定；真实主体矩阵管理 API 与双身份对话仍待复测。
**用户直接验证：**
1. 查询 /subjects，按昵称/用户名筛选；给主体绑定测试数据集。
2. 按主体查数据集，按数据集查主体，重复同一绑定并确认没有重复项。
3. 解除绑定，用同一主体新发一次知识问题，检查工具是否仍使用已解除的库。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P09`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 矩阵两侧结果一致，分页与总数正确；绑定/解绑影响下一次工具调用，不要求用户重启服务。

**失败/异常必须验证：** 不存在的数据集不能生成无效绑定；主体无数据集时有清晰空结果，而不是编造知识来源。

**当前源码起点：** [list_agent_subjects_logic.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/admin/list_agent_subjects_logic.go)。
### P10｜把文档启停改为可重试的明确状态操作

**阶段 / 类型：** B / 完善现有接口　　**前置：** P06　　**状态：** 已完成本地契约验收；真实 HTTP/重启联调待复测

**拆分交付：**
- [x] 使用数据集 + 文档联合定位，补父子路径不一致的校验。
- [x] 请求必须携带明确的 `enabled=true/false` 目标值，重复请求不会再次翻转；API 类型、handler、Swagger 已同步。
- [x] 停用只改变可见状态，不删除正文、分块或索引内容；启用后恢复召回。
- [ ] 在真实 HTTP 进程中验证重启后状态仍保持，并补真实 PostgreSQL/检索服务联调记录。

**用户直接验证（本地确定性验收）：**
1. 执行 `bash "$REPO/scripts/acceptance/run.sh" P10`，确认启停状态机、父子路径保护和检索可见性测试通过。
2. 手工调用 `PATCH /api/v1/dataset/{id}/documents/{docId}/enabled`，请求体使用 `{"enabled":false}`；完全相同请求重复两次，状态仍为 `false`。
3. 读取正文确认内容仍在；用错误的数据集路径操作同一个 `docId`，应返回资源不存在/不匹配，原文档状态不变。
4. 使用 `{"enabled":true}` 重复两次，再搜索固定词，确认文档重新可召回。

**验收入口（完成本地项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P10`；报告目录会写入 `logs/acceptance/P10/`，包含测试日志和 `summary.json`。

**通过标准：** 重复请求始终维持目标状态；停用不召回、正文仍可读；启用可召回；父子路径不一致被拒绝且文档未改变。

**失败/异常必须验证：** 缺失 `enabled` 字段不能静默当成 `false`；不存在的文档明确报错；超时重试不能把停用变回启用。旧客户端若仍发送翻转式请求，必须迁移为显式目标值，当前不再兼容隐式翻转。

**当前源码起点：** [enable_document_logic.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/dataset/enable_document_logic.go)。

### P11｜补无 ES 时的结构化精确检索

**阶段 / 类型：** B / 补齐已有缺口　　**前置：** P06　　**状态：** 已完成本地契约验收；真实 API/PG/规模性能待复测

**拆分交付：**
- [x] 实现可配置字段的结构化相等匹配，覆盖 mapping 中声明的 model/product_id/系列/品类/variants；统一大小写、首尾和连续空格规则。
- [x] 复用 `configs/es/chunk_mapping.yaml` 的字段声明，PG 回落不再维护第二份业务字段清单。
- [x] 补无 ES 情况下 exact 通道状态、排序融合、enabled/dataset 过滤和多数据集用例。
- [x] 增加独立验收入口、报告和用户手工验证步骤。

**用户直接验证：**
1. 关闭 ES 测试配置，导入型号 ACC-H105P 与 ACC-H105PE 两条产品。
2. 搜索完整型号 ACC-H105P，查看精确命中及融合后的结果。
3. 搜索部分型号 ACC-H105，再查询不存在的完整型号。

**验收入口（完成本地项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P11`；详细步骤见 `docs/p11-exact-search-acceptance.md`。本地报告只证明契约，真实 API/PG/规模性能另行留档。

**通过标准：** 完整型号进入 exact；近似型号不能被记录为精确命中；子串命中可由 keyword 返回，但不能冒充 exact。

**失败/异常必须验证：** 结构化通道失败要进入降级台账；不可只把原来的子串匹配改名为 exact。

**当前源码起点：** [store.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/store.go)。

### P12｜完善多文件上传、大小限制与失败提示

**阶段 / 类型：** B / 完善已有上传　　**前置：** P04　　**状态：** 已完成代码与本地契约验收；真实依赖联调待复测

**已交付：**
- [x] 保留 multipart 多文件能力，`file` 可重复；第一版明确只接收 `.md`、`.markdown`、`.txt`、`.text`，不宣称支持 PDF/Office。
- [x] 增加 `Knowledge.MaxUploadRequestBytes`、`Knowledge.MaxDocumentBytes`、`Knowledge.MaxUploadFiles`，API/验收配置分开维护；默认分别为 50 MiB、5 MiB、10 个文件。
- [x] 单文件使用 `io.LimitReader(max+1)` 后再判断，避免因为 `ParseMultipartForm(8 MiB)` 误以为已有单文件硬上限。请求总量同时检查 `Content-Length` 和实际读取量。
- [x] 空文件、坏扩展名、单文件超限、缺少 `file`、请求超限和文件数超限都有明确错误。
- [x] 响应保留 `data` 兼容旧客户端，新增 `files[]` 与 `partial_failure`；逐文件失败不会吞掉同批成功项，调用方可只重传 `status=failed` 的文件。
- [x] 成功/失败路径都清理 multipart 临时文件；不对普通文档重传做超出源码依据的“任意幂等”承诺。

**用户直接验证：**
1. 运行 `bash "$REPO/scripts/acceptance/run.sh" P12`，确认本地契约通过并查看 `logs/acceptance/P12/<时间>-<pid>-local/summary.json`。
2. 准备两个小于 5 MiB 的 Markdown/文本文件，执行：
   `curl -X POST "$BASE/dataset/$DATASET_ID/documents/upload" -H "Authorization: Bearer $ADMIN" -F "file=@样例A.md" -F "file=@样例B.txt"`。
3. 检查返回：`data` 是成功文档扁平列表，`files` 有两个 `succeeded` 项；等待 worker 后再用文档列表/搜索确认索引。产品型录可能一文件拆成多条 `data`，不能用返回条数等于文件数断言。
4. 再混入空文件、`.pdf` 文件和超过 `Knowledge.MaxDocumentBytes` 的文件；确认成功文件仍在 `data`，失败项在 `files[].error` 中分别写出 `empty_file`、`unsupported_format`、`payload_too_large`。
5. 仅重传失败文件；不要把已经成功的整批盲目重传。若需要验证总请求/文件数上限，可按配置生成超过 50 MiB 或超过 10 个 `file` 字段的请求。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P12`；上述真实 API 步骤需要在用户的 API/worker/Redis/数据库环境中执行并另行留档。

**通过标准：** 正常多文件可用，超限请求明确报错；部分成功有逐文件台账；既有成功项可定位，不盲目重传整个批次。

**失败/异常必须验证：** 错误不能变成通用 500；失败不残留无限增长的临时文件；普通文档未有稳定查重键时不得声称任意重传都幂等。

**当前源码起点：** [upload_document_logic.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/internal/logic/dataset/upload_document_logic.go)、[dataset.api](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/transport/restapi/docs/dataset/dataset.api)。

### P13｜交付目录批量导入和增量重跑

**阶段 / 类型：** B / 新增导入功能　　**前置：** P12、P05　　**状态：** 本地契约完成；真实依赖联调待用户环境执行

**拆分交付：**
- [x] 提供独立 `cmd/knowledge-import` CLI，递归扫描 Markdown/纯文本，生成稳定逻辑 source 和内容指纹。
- [x] 提供 dry-run、逐文件结果、失败清单与 `--retry-report` 重试；不默认删除已入库但目录中消失的文档。
- [x] 交付递归目录夹具和根目录边界测试；外部软链接按文件失败，不读取根目录外内容。

**实现切入点：**
- 扫描与边界：`internal/application/knowledge/directory_import.go` 的 `scanImportFiles`。
- 增量判定：按稳定 `source` 找已有文档，再对正文计算 SHA-256；相同为 `unchanged`，变化为 `updated`。
- 写入复用：正式导入调用现有 `Service.Create/Update`，由既有 queue 投递 worker；导入器不复制索引代码。
- 可恢复性：报告的 `files[].status=failed` 路径可通过 `--retry-report` 只重试失败项。
- 删除语义：源目录缺失只计入 `missing_not_deleted`，不自动删除数据库文档。

**用户直接验证：**
1. 对三份测试文件执行 dry-run，确认计划新增数且数据库没有改变。
2. 正式导入等待 ready，再原样导入；只修改一份文件后再次导入。
3. 加入一个坏文件，查看失败清单，修正后仅重试失败项。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P13`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 首次新增 3 份；原样重跑不新增、不无谓排队；改单文件只更新该项；失败和成功项分别可查。

**失败/异常必须验证：** 坏文件不让整个目录静默成功；软链接或相对路径不能导致读取测试根以外的资料。

**当前源码起点：** [service.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/service.go)。

### P14｜接入真实 Rerank，并能查看排序前后差异

**阶段 / 类型：** B / 新增检索能力　　**前置：** P20　　**状态：** 已完成本地 HTTP 适配器契约；真实供应商与模型效果待隔离环境

**开始前需确认：** 开始前确定重排服务、调用预算和配对评测标准；不在计划中预选外部供应商。

**拆分交付：**
- [ ] 实现具体 reranker，在 HTTP 与评测入口注入，`Retrieval.EnableRerank` 真正控制排序。
- [ ] 记录可脱敏核对的候选 ID、RRF 排名、重排排名和服务配置摘要。
- [ ] 准备固定候选的确定性排序测试，以及真实重排服务的配对评测。

**用户直接验证：**
1. 对同一数据快照和问题集分别关闭、开启重排，运行 P14 并比较结果。
2. 查看固定候选用例里预期文档的排名变化，确认 API 与评测入口都生效。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P14`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 开关影响实际调用和排序；报告展示前后对应关系；真实服务达到验收前约定的质量改善/不退化标准。

**失败/异常必须验证：** 缺少必要重排配置不能静默当成启用；模拟 ranker 的排序变化不能冒充真实模型质量改善。

**当前源码起点：** [retriever.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/retriever.go)。

### P15｜补 Rerank 候选预算、超时与降级

**阶段 / 类型：** B / 完善新增能力　　**前置：** P14　　**状态：** 已完成本地契约；真实服务故障演练待隔离环境

**拆分交付：**
- [ ] 让 `Retrieval.MaxRerankCandidates` 真正限制候选数，交付可观察的实际送入数量。
- [ ] 明确整体/通道时间预算；重排超时或失败时保留原始 RRF 排名并说明降级原因。
- [ ] 使用隔离测试代理模拟慢响应和错误，不修改真实线上服务。

**用户直接验证：**
1. 把候选上限设为一个小值，运行包含更多候选的查询，查看送入计数。
2. 分别模拟重排超时、错误和恢复，再查询同一问题。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P15`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 实际重排数不超过上限；失败后仍有原始召回结果和降级信息；响应在声明的时间预算内结束。

**失败/异常必须验证：** 不允许无限等待或静默返回空结果；恢复正常后无需重启才能重新使用重排。

**当前源码起点：** [retriever.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/retriever.go)。

### P16｜接入 LLM 产品拆分，支持无 front-matter 原文

**阶段 / 类型：** B / 可选增强　　**前置：** P05、P20　　**状态：** 已完成本地契约；真实模型与金标待确认

**开始前需确认：** 确定使用的模型、费用/时限和标注样例；这是增强路线，不阻塞现有结构化型录首轮验收。

**拆分交付：**
- [x] 保留现有结构化型录路径，新增 API 可配置的 provider-neutral HTTP 拆分路径，限定产品数、响应体、请求超时。
- [x] 模型结果在写库前校验稳定键、正文、重复型号和数量上限，再转换为标准产品文档规格。
- [ ] 交付并复核人工标注的双产品原始文本、单产品文本与不可拆分样例；真实模型质量仍待确认。

**用户直接验证：**
1. 上传不带 YAML 头但明确描述两个产品的固定资料，等待索引完成。
2. 查询文档列表、两份正文与对应元数据，逐项对照人工标注。
3. 上传单产品资料和无法可靠识别的资料，检查结果或明确提示。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P16`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 本地契约保证双产品结构化响应可产生两条可独立写入的文档，老 front-matter 导入不受影响；真实模型还需人工金标确认元数据不捏造规格。

**失败/异常必须验证：** 模型超时、无效格式或边界不清时明确失败；不得静默退化为一份看似成功的低质量产品文档。

**当前源码起点：** [product.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/rag/parser/product.go)。

### P17｜确保 LLM 拆分可重放和并发重传不重复

**阶段 / 类型：** B / 完善可选增强　　**前置：** P16　　**状态：** 已完成本地指纹/稳定键契约；真实并发重放待联调

**拆分交付：**
- [x] 以正文输入摘要、模型和解析配置版本生成可追溯指纹，并将解析版本写入产品元数据。
- [x] 复用现有 external_key 唯一约束、冲突重查和增量 upsert，避免并发同型号增生文档。
- [x] 版本变化会改变指纹并触发更新；真实模型缓存/显式重新解析入口与进程重启联调仍待补。

**用户直接验证：**
1. 同一份双产品资料串行上传三次、并发上传两次，比较文档 ID、型号和正文指纹。
2. 重启相关进程再重传；检查文档数和额外索引任务。
3. 改变解析版本并执行显式重新解析，检查结果版本和更新清单。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P17`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 同版本相同输入不增生文档、不重复制造相同任务；重启后仍可复用；换版变化可追溯。

**失败/异常必须验证：** 失败结果不能被当成成功缓存；并发结果分歧必须明确收敛，不能两个版本同时落成重复文档。

**当前源码起点：** [split.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/knowledge/split.go)。

### P18｜让指定默认 Agent 真正使用 Skill

**阶段 / 类型：** B / 补齐已有接线　　**前置：** P07　　**状态：** 已完成本地接线与边界契约；真实模型工具记录待隔离环境

**拆分交付：**
- [x] 明确最小授予范围：只给 workspace specialist 只读 Skill 工具，不扩大写入/执行工具集合。
- [x] 授予 list_skills/load_skill，同步指令要求先列出再加载，并编写验收说明。
- [ ] 在真实模型请求中保存可核对的工具调用证据，确保不是模型声称“用了 Skill”。

**用户直接验证：**
1. 安装带唯一标题和输出格式要求的验收 Skill，向指定 Agent 发起对应任务。
2. 查看工具记录，确认先列出/加载 Skill，再读取固定文件并按格式返回。
3. 移除该验收 Skill 后重复提问。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P18`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 工具记录包含实际加载，回答遵守固定格式；移除后明确无法加载；未授予的 Agent 不被隐式扩权。

**失败/异常必须验证：** 缺失或超大 Skill 明确提示；Skill 内容不能绕过既有操作审批和工作区边界。

**当前源码起点：** [factory.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/application/agent/factory.go)。

### P19｜接入外部用户同步，而非只保留 501

**阶段 / 类型：** B / 本地契约已完成、真实上游条件性验收　　**前置：** P02、P09　　**状态：** 本地实现完成，真实上游待条件确认

**已交付（2026-10-07）：**
- [x] 替换 501 占位：provider-neutral HTTP source、`Auth.UserSync` API-only 配置与管理员 `dryRun` 请求。
- [x] 使用可空唯一 `external_id` 做稳定匹配；报告 `created/updated/skipped/failed`，支持重复同步幂等。
- [x] 明确 no-delete 边界：上游暂时缺人不会删除本地账号、权限或会话；同名手工账号不会被自动认领。
- [x] 新同步账号生成一次性初始密码并保持 `must_change_password=true`；上游不能覆盖本地 role/status/password；新增显式迁移 `0003_user_external_id`。
- [ ] 真实上游接通仍需确认 endpoint/协议/分页、稳定外部 ID、验收凭据和密码下发方案；本地 fake source 不能替代真实验收。

**用户直接验证：**
1. 本地执行 `bash scripts/acceptance/run.sh P19 --local`，打开 `tests.log` 与 `summary.json`，确认 dry-run 不写库、重复同步只计 skipped、缺人不删除。
2. 真实上游条件具备后：新增一名用户，先 dry-run 再正式同步；改昵称后二次同步；原样第三次同步，核对计数。
3. 模拟上游不可达、重复 `external_id`、用户名冲突与暂时少一名用户，确认错误可定位且本地用户与会话保留。

**验收入口：** `bash "$REPO/scripts/acceptance/run.sh" P19 --local`；真实上游验收仍需把 source 配置、凭据来源、逐项台账与依赖快照写入报告。

**通过标准：** 本地契约通过；真实环境中稳定匹配不重复建号，失败不会假报成功，缺人不会删除本地账号或权限。

**失败/异常必须验证：** 不可达或无效用户数据有逐项结果；凭据不进入日志/报告；fixture 通过不能标记真实上游已接通。

### P20｜交付真实召回基线和一键复测

**阶段 / 类型：** C / 验证已有评测能力　　**前置：** P06、P05　　**状态：** 已完成本地评测契约；真实召回基线待隔离依赖环境

**拆分交付：**
- [x] 本地校验 JSONL 金标的装载、重复 ID、空查询、绝对 source 和阈值配置错误。
- [x] 本地校验 Recall/MRR/命中率/P95、逐用例失败记录、预算/粒度口径和报告明细渲染。
- [x] 提供一键本地入口和独立 `summary.json`；报告明确标出真实 PostgreSQL/Milvus/ES/embedding 尚未执行。
- [ ] 在隔离真实环境对齐金标与语料，保存版本、语料/用例指纹、模型与维度、切块/归并策略、通道配置和阈值。
- [ ] 固定真实数据与配置重复运行，给出逐用例命中及实测 Recall/MRR/P95，区分基线记录与质量达标。

**从哪里切入：** 先执行 `bash scripts/acceptance/run.sh P20`，确认评测输入/阈值/报告契约没有问题；再按 P02 预检准备隔离 PostgreSQL、Milvus、ES、embedding 和已 ready 语料，最后执行 `go run ./cmd/rag-test -config <隔离配置> -out <报告目录>`。

**用户直接验证：**
1. 本地先执行 `bash scripts/acceptance/run.sh P20`，打开输出目录中的 `tests.log` 与 `summary.json`。
2. 真实环境准备完成后执行现有 rag-test 命令及 P20 包装验收，核对逐用例期望、命中 source 和总体 Recall/MRR/P95，然后重复运行比较。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P20`；本地入口只证明评测契约，真实基线仍必须把 JSON 报告、命令退出码和依赖快照写入报告。

**通过标准：** 本地评测输入和报告契约稳定；真实环境中用户可复测同一语料并得到可解释报告；门槛达标/未达标如实展示；模型/数据配置没有暗中变化。

**失败/异常必须验证：** 缺语料、无索引或报告不能复现时真实验收不通过；质量未达标必须列出整改用例，不通过降低阈值掩盖。仅有本地测试不能声称真实 Recall/MRR/P95 已完成。

**当前源码起点：** [main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/rag-test/main.go)。

### P21｜补私有语料的可见性隔离评测

**阶段 / 类型：** C / 补齐评测盲区　　**前置：** P09、P20　　**状态：** 已完成本地隔离契约；真实主体矩阵待环境复测

**拆分交付：**
- [x] 提供两份不同测试主体的非空合成私有文档金标，正向 source 与 forbidden source 成对出现。
- [x] 本地评测在注入跨主体泄漏时必须失败，并记录具体泄漏 source 与非零 ACL 计数；动态绑定回归确认主体变更即时生效。
- [ ] 在真实 API、对话工具、exact/keyword/vector 通道上用两名测试主体复测服务端过滤范围。

**用户直接验证：**
1. 分别用两名正常测试用户查询各自知识库和公共资料。
2. 在启用 ES 与回落配置下重复，检查响应及引用没有另一人的私有内容。
3. 检查报告中执行的私有样例数量和泄漏统计。

**验收入口：** 本地执行 `bash "$REPO/scripts/acceptance/run.sh" P21`；真实复测仍需把主体、非空私有文档、请求和泄漏统计写入报告。

**通过标准：** 允许访问的合成资料正常返回；不允许返回的私有资料不出现在正文、引用或元数据中；真实隔离用例数大于 0。

**失败/异常必须验证：** 任意泄漏都导致验收及质量门禁失败；不能因查不到任何资料就认为隔离成功。

**当前源码起点：** [runner.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/eval/runner.go)。

### P22｜补知识问答无答案与引用正确性验收

**阶段 / 类型：** C / 新增回答质量验收　　**前置：** P07、P20　　**状态：** 已完成本地回答证据契约；真实模型问答待环境复测

**拆分交付：**
- [x] 提供确定性回答评测器，覆盖必需事实、禁止事实、明确无答案和引用格式。
- [x] 校验引用 source 必须属于授权候选，缺引用、越权引用和无答案编造均失败。
- [ ] 在真实模型上补有答案、无答案、冲突资料和检索依赖失败四组人工标注问题，确定重复轮数并保存脱敏问答对照表。

**用户直接验证：**
1. 问一个测试语料有答案的问题，逐项对照引用的正文。
2. 问明确不在语料中的规格，再问含冲突来源的事实。
3. 在隔离环境让检索不可用，观察回答与错误提示。

**验收入口：** 本地执行 `bash "$REPO/scripts/acceptance/run.sh" P22`；真实模型复测必须另存回答、引用原文和人工判定，不能以本地规则测试代替。

**通过标准：** 有答案时事实与引用一致；无答案不捏造规格；冲突资料明确说明；依赖失败不会伪装成正常无答案。

**失败/异常必须验证：** 虚构来源、引用不支持结论或混淆服务故障时失败；仅靠另一模型评分不能代替标注用例和用户核对。

**当前源码起点：** [knowledge.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/tool/knowledge.go)。

### P23｜交付 CI 质量门禁和跨版本报告对比

**阶段 / 类型：** C / 新增持续验证　　**前置：** P01、P03、P20、P21、P22　　**状态：** 已完成本地质量门禁；CI 平台与真实评测接入待确认

**开始前需确认：** 需确认 CI 平台、隔离测试服务和报告保留周期；未决定前先交付本地等效检查，不冒充已接流水线。

**拆分交付：**
- [x] 提供 `scripts/quality-gate.sh` 和 `make quality-gate`，串行运行全量 Go 回归、API 契约、P20、P21、P22 本地门禁。
- [x] 门禁失败返回非零并保留分项日志与脱敏 `summary.json`；真实评测依赖明确标记为未执行，不伪装成通过。
- [ ] 选定 CI 平台，接入隔离依赖/受控凭证、报告归档和跨版本配置指纹对比。

**用户直接验证：**
1. 用正常版本触发流水线，下载报告并复核关键指标。
2. 在测试分支加入必然失败的用例，确认门禁失败且发布阶段不执行；之后移除该测试变更。
3. 选择两个同配置版本进行对比，再尝试不同 embedding 配置的报告。

**验收入口：** 本地执行 `bash "$REPO/scripts/acceptance/run.sh" P23`；CI 与真实质量门禁需在确定平台和隔离依赖后再验收。

**通过标准：** 正常版本满足约定门槛才放行；失败明确阻断；报告可下载、可追溯；不相容配置不被当作质量退化直接比较。

**失败/异常必须验证：** 依赖或凭证缺失应标记阻塞/失败，不能当作通过；若 P20 质量不达标，发布门禁保持失败并列整改。

**当前源码起点：** [thresholds.yaml](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/eval/thresholds.yaml)。

### P24｜启动维护清理任务并交付 dry-run

**阶段 / 类型：** D / 补齐已有接线　　**前置：** P08　　**状态：** 已完成本地 dry-run/报告契约；真实数据库保留期演练待隔离环境

**拆分交付：**
- [x] 为现有维护 Worker 提供独立入口，复用 worker 配置中的周期/批量/保留期；`-periodic` 先执行一次再按周期运行。
- [x] 提供 dry-run、执行计数和 JSON 清理报告，仅对终态/已过期记录生效。
- [ ] 在独立验收数据库里准备应清理和应保留记录，不在生产库缩短保留期做实验。

**用户直接验证：**
1. 先 dry-run，看到待清理分类/数量并确认记录未改变。
2. 执行一次清理，核对过期记录减少而活跃记录保留。
3. 启动周期任务，等待一次执行后再次查看报告；原样重跑确认无重复副作用。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P24`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 清理真正发生，不只是配置存在；保留边界正确，幂等重跑；批量任务有完成统计。

**失败/异常必须验证：** 数据库失败报告明确，后续周期可恢复；不得删除活跃审批或无确认地清理实际业务数据。

**当前源码起点：** [worker.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/maintenance/worker.go)。

### P25｜交付索引/检索业务指标与分级健康信息

**阶段 / 类型：** D / 新增运行可观测性　　**前置：** P01、P04、P06　　**状态：** 已完成本地业务指标/队列采样契约；真实 Prometheus 抓取与依赖故障演练待隔离环境

**拆分交付：**
- [x] 导出队列积压、索引成功/失败/重试、通道命中/降级与检索耗时指标。
- [x] 用 component_health 区分检索 ready、degraded、unavailable；HTTP liveness/数据库 readiness 仍由真实部署探针联调确认。
- [x] API 与 worker 启动时把业务 collectors 注册到 go-zero Prometheus agent 使用的默认 registry，业务指标不再只存在进程内测试 registry。
- [x] 提供本地指标 gather/验收入口，标签只使用队列名、状态和固定通道，不以正文/用户 ID 作为指标标签。

**用户直接验证：**
1. 抓取指标基线，上传测试文档并搜索，再抓取比较计数和耗时样本。
2. 暂停测试 worker 形成积压，再恢复观察积压下降。
3. 禁用/中断测试 ES，查看健康信息、降级指标与告警条件。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P25`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 指标随实际操作变化；积压/恢复可观察；读写能力与降级状态解释准确；日志之外有可抓取业务信号。

**失败/异常必须验证：** 无 ES 正常部署不能持续误报严重故障；核心写依赖失效不能仍宣称写入健康。

**当前源码起点：** [metrics.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/observability/metrics.go)。

### P26｜验收 HTTP→队列→worker 的完整追踪

**阶段 / 类型：** D / 完善追踪闭环　　**前置：** P04、P25　　**状态：** 已完成本地跨进程 trace 契约与 collector 配置；真实 collector/UI 联调待隔离环境

**拆分交付：**
- [x] HTTP/worker 共用 OTLP 传播与独立服务名配置，队列 envelope 携带 W3C traceparent。
- [x] 提供 `configs/observability/otel-collector.yaml` 和自动化 trace ID 关联检查。
- [x] span 属性只记录任务类型、队列和 ID 类低敏元数据；本地检查明确禁止凭证/正文进入 collector 配置。

**用户直接验证：**
1. 开启测试 collector，上传文档直到 ready，查看报告提供的 trace ID。
2. 在追踪界面确认 HTTP、投递、消费、索引跨进程落在同一 trace。
3. 模拟任务重试和 collector 不可达后重跑。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P26`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 关键 span 可关联到同一 trace，重试可辨认；collector 不可达不阻断正常业务或无限拖延进程退出。

**失败/异常必须验证：** 只出现 HTTP span 或 worker 另起不相关 trace 判定未完成；报告不得包含请求凭证/正文敏感数据。

**当前源码起点：** [trace.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/queue/asynq/trace.go)。

### P27｜用版本化迁移替代生产启动自动改表

**阶段 / 类型：** D / 生产化必需项　　**前置：** P04　　**状态：** 已完成本地显式迁移/安全启动边界；真实数据库升级与恢复演练待隔离环境

**拆分交付：**
- [x] 交付 `internal/platform/storage/entx/migrations/` 版本化迁移文件、`schema_migrations` 状态查询、dry-run 与 `cmd/db-migrate` 部署入口。
- [x] 区分开发 `OpenDevelopment` 自动迁移与生产 `OpenWithoutMigration` 连接；应用启动不隐式执行删列/删索引。
- [ ] 在空库和带合成历史数据的数据库副本验证升级、重跑及备份恢复。

**用户直接验证：**
1. 查看待执行变更 dry-run，再对空验收库执行迁移并启动服务。
2. 在历史数据副本升级，核对既有文档/会话仍可读取；再次执行迁移。
3. 启动两个应用实例确认不抢着执行 DDL，再执行交付的副本恢复演练。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P27`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 迁移版本可查，重跑不重复执行；应用启动无未审核 DDL；数据保留，备份恢复在副本上验证成功。

**失败/异常必须验证：** 迁移失败阻断发布并明确恢复步骤；不可用真实数据库验证删列，也不能假设所有 DDL 都可以直接回滚。

**当前源码起点：** [database.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/storage/entx/database.go)。

### P28｜交付影子索引重建，旧索引仍可服务

**阶段 / 类型：** D / 新增索引版本能力　　**前置：** P20、P25、P27　　**状态：** 已完成本地影子索引版本控制平面；真实 ES/Milvus 重建待隔离环境

**拆分交付：**
- [x] 为 embedding 模型/维度和索引配置建立不可变版本元数据；控制平面按版本 ID 隔离状态。
- [x] 提供本地影子版本创建、进度、ready/failed 门禁、状态查询与显式切换入口，默认活动版本保持不变。
- [ ] 将控制平面接入真实 ES/Milvus 独立集合重建、逐版本评测和完整性校验。

**用户直接验证：**
1. 记录当前活动版本和检索基线，创建新版并开始影子重建。
2. 重建期间持续查询旧版，确认不中断；等待新版完成。
3. 对新版单独跑评测，比较条目数、索引进度、版本指纹和质量报告。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P28`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 重建不覆盖旧集合，旧版持续可查询；新版状态可查且能独立评测；默认版本尚未改变。

**失败/异常必须验证：** 新版构建失败不破坏旧版；维度或语料不匹配时不能混合查询或标记已完成。

**当前源码起点：** [main.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/worker/main.go)。

### P29｜交付索引切换、回滚与故障恢复演练

**阶段 / 类型：** D / 完善索引版本能力　　**前置：** P28、P23　　**状态：** 已完成本地切换/回滚安全契约；真实多实例切换演练待隔离环境

**拆分交付：**
- [x] 提供显式 switch/rollback 入口，只有 ready 版本可激活；失败版本和 active 版本不能被错误标记。
- [x] 状态持久化、重复 status/switch/rollback 的本地幂等安全契约已覆盖；旧版本不会随切换删除。
- [ ] 接入真实多实例活动版本传播、请求级一致性和重建失败/切换失败/回滚演练。

**用户直接验证：**
1. 切换到合格新版，查询固定问题并确认活动版本一致。
2. 回滚旧版，确认检索版本和结果恢复；重复操作验证状态幂等。
3. 尝试切换到未完成/不合格版本，模拟一个实例延迟更新配置。

**验收入口（完成本项后交付）：** `bash "$REPO/scripts/acceptance/run.sh" P29`；上述手工步骤也必须写入报告，允许用户复查。

**通过标准：** 合格版本可切换并可回滚；旧版本仍可用；实例状态可核对，不合格版本无法成为活动版。

**失败/异常必须验证：** 任何切换失败保留最后有效版本；清理旧索引是另行确认的动作，不能随切换自动删除回滚依据。

**当前源码起点：** [retrieval.go](/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/cmd/restapi/retrieval.go)。

## 5. 可选需求：确认后另开工作包，不偷偷混入本轮

以下事项目前不是已有闭环的明确缺陷，或属于产品范围扩展，不应借“生产化”名义直接改动：

| 候选需求 | 为什么暂不承诺 | 确认后怎样拆成可验收项 |
| --- | --- | --- |
| 数据集名称/说明编辑、状态开关 | 当前缺完整编辑接口，但产品具体需求和停用语义尚未确认 | 新增编辑接口→查询确认变更；停用后验证索引/搜索/绑定行为，单独定义兼容边界 |
| 用户列表分页及更完整生命周期 | 当前列表不分页，其余管理操作需产品确认 | 先交付分页和关键字组合验证；其他操作各拆独立接口与验收，不一次扩大范围 |
| 独立前端验收页面 | 本次未审阅或验证前端，不把“有页面”视为已完成 | 每个页面以已稳定的后端计划为前置，交付可点选步骤与 API 对照 |
| PDF/Office 摄取、超大文件对象存储 | 当前主路径为 Markdown/文本，并非任意格式解析系统 | 每种格式一个导入样例，验收文本保真、分块与引用；不要只验证上传成功 |
| 工具链依赖升级 | 当前提示不等于业务失败，升级有回归风险 | 单独的兼容性任务，在目标工具链上完成全量测试及 P04/P07 主链路回归 |

任何新增工作包都应沿用本文件的结构：**实现交付 + 用户步骤 + 预期结果 + 异常场景 + 报告**。

## 6. 用户验收记录模板

每完成一项，用下表记录，避免把“代码已合并”当作“用户已经验收”：

| 计划编号 | 交付版本 | 开发自测 | 用户复测 | 证据位置 | 最终状态/问题 |
| --- | --- | --- | --- | --- | --- |
| P__ | commit / 分支 | 通过 / 失败 / 阻塞 | 通过 / 不通过 / 未验证 | logs/acceptance 的本轮目录或 CI 产物 | 待开发 / 待验收 / 已验收 / 需返工 |

用户确认一个工作包时只需要回答四个问题：

1. 我能按说明运行，不需要临时改代码吗？
2. 正常输入的 API/文件/报告结果与预期一致吗？
3. 坏输入或故障能得到明确结果，而不是静默成功吗？
4. 重跑后结果可解释，没有多出重复数据或不可控副作用吗？

任一项答案为否，该工作包保持“待验收/需返工”，先记录实际问题；不能因为后续计划很多就跳过。

## 7. 阶段验收出口

| 阶段出口 | 必须提供的用户可见交付 | 不代表什么 |
| --- | --- | --- |
| 已有链路可联调 | 测试全绿；上传/索引/搜索/对话/审批/绑定的复测报告 | 不代表所有真实模型质量达标或生产可发布 |
| 关键增强可使用 | 本次选择的 P10–P19 逐项验收；重排/拆分有效果与错误边界 | 未确认的外部用户源、模型服务不能自动算完成 |
| 质量可持续验证 | 真实召回基线、私有/无答案用例、CI 阻断与跨版本报告 | 不代表阈值永远适合业务，仍需维护金标 |
| 生产运行可恢复 | 维护、业务指标、跨进程追踪、迁移、索引切换和回滚演练 | 不替代实际部署容量评估与运维确认 |

**推进建议：一次完成并提交一个工作包，交付同编号验收入口和报告，让用户复测确认后再推进下一个；无需等全部 29 项完成才让用户看到效果。**

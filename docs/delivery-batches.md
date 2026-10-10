# 四个交付批次与直接验收入口

更新日期：2026-10-07。

这份文档把现有 P01–P29 收敛成四个可独立验收的目标。每个目标都有一个聚合命令；命令结束后会输出本批次报告目录和 `summary.json`。本地批次只验证确定性代码、配置、协议和故障边界，不把 fake 服务或单元测试冒充真实 PostgreSQL、Redis、Milvus、ES、模型和多实例验收。

## 目标一：能用、能复测

**包含计划：** P01、P02、P03、P04、P05、P06、P07、P08、P09、P12、P20。

**本地直接验证：**

```bash
bash scripts/acceptance/run-batch.sh 1 --local
```

或通过统一入口：

```bash
bash scripts/acceptance/run.sh batch 1 --local
```

**本地通过标准：**

- 指标启用/禁用和全量 Go 回归通过。
- API / worker 配置隔离、配置预检和仓库配置文件检查通过；本地批次不依赖 Docker。
- API 生成物与契约一致。
- 文档上传、索引状态、更新、重建、删除的确定性验收通过。
- 产品拆分、重复上传、三通道召回、SSE、审批、评测契约通过。

**真实出口：**

不要求使用本地 Docker。直接复用已有部署和仓库中的业务配置；默认配置已经指向现有的 PostgreSQL、Redis、Milvus、OTLP 和可选 ES 地址，密码/模型密钥仍从环境变量读取：

```bash
export EINO_API_CONFIG="$PWD/configs/api/config.yaml"
export EINO_WORKER_CONFIG="$PWD/configs/worker/config.yaml"
export EINO_REST_CONFIG="$PWD/configs/api/restapi.yaml"

# 只做配置、健康、数据库、Redis、Milvus、worker 和 embedding 预检。
bash scripts/acceptance/run.sh P02 --existing --degraded

# 已有 API/worker 正在运行后，执行真实 HTTP 生命周期；不要让脚本自行启动服务。
export EINO_ACCEPTANCE_ISOLATED=1
export EINO_ACCEPTANCE_BASE="http://127.0.0.1:8090/api/v1"
export EINO_ACCEPTANCE_TOKEN="$EINO_API_KEY_ADMIN"
bash scripts/acceptance/run.sh P04 --cleanup

# 已有索引和固定语料准备好后，使用同一业务配置跑召回基线。
go run ./cmd/rag-test -config "$EINO_API_CONFIG" -out logs/eval-report.json
```

真实出口要求 PostgreSQL、Redis、Milvus、worker、embedding 和固定语料可用；ES 完整模式再执行 `P02 --existing --complete`。`configs/acceptance/` 和 `P02-deps` 仅保留为需要独立隔离环境时的可选模板，不是本批次的前置条件。

## 目标二：质量可持续

**包含计划：** P21、P22、P23、P27。

**本地直接验证：**

```bash
bash scripts/acceptance/run-batch.sh 2 --local
```

**本地通过标准：**

- 私有语料隔离测试能发现注入的越权样例。
- 无答案、必需事实、禁止事实和引用授权边界通过。
- 质量门禁能在好版本放行、坏版本阻断。
- 版本化数据库迁移的 dry-run、重复执行和启动边界通过。

**真实出口：**

在隔离数据库副本执行历史数据迁移、备份恢复和真实主体矩阵评测；CI 平台接入由实际凭据和报告归档条件决定。

## 目标三：按需要补齐能力

**包含计划：** P10、P11、P13、P14、P15、P16、P17、P18、P19。

**本地直接验证：**

```bash
bash scripts/acceptance/run-batch.sh 3 --local
```

**本地通过标准：**

- 文档启停可重试且不会翻转状态。
- 无 ES 时仍能执行结构化精确检索。
- 目录导入支持边界、dry-run、增量和失败重试。
- provider-neutral Rerank 与 LLM 拆分 HTTP 合同、候选预算、超时和降级通过。
- Skill 接线、LLM 重放指纹和用户同步本地边界通过。

**真实出口：**

真实 Rerank、LLM、Skill 工具调用和外部用户源需要先确认服务地址、模型/协议、预算、凭据和审计字段，再分别运行 P14–P19 的真实验收；本地 HTTP adapter 不等于供应商效果通过。

## 目标四：运行可恢复

**包含计划：** P24、P25、P26、P28、P29。

**本地直接验证：**

```bash
bash scripts/acceptance/run-batch.sh 4 --local
```

**本地通过标准：**

- 维护任务 dry-run、保留期、批量清理和报告通过。
- 业务指标、队列采样、检索降级和健康状态保持低基数且可抓取。
- HTTP 到 worker 的 trace envelope、collector 配置和脱敏边界通过。
- 影子索引构建、进度、ready、切换、回滚和失败状态机通过。

**真实出口：**

需要真实 Prometheus / OTLP collector、暂停 worker、队列重试、ES/Milvus 独立索引、多实例和失败切换演练。

## 报告位置

聚合报告写入：

```text
/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/batch-<1|2|3|4>/
```

每次报告包含：

- `summary.json`：本批次通过/失败及每项结果。
- `<plan>.log`：对应编号验收的原始输出。

## 当前本地验收结果

2026-10-07 已完成四批次本地聚合验收：

- 第一批：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/batch-1/20261007-154134-18619-local`
- 第二批：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/batch-2/20261007-154427-21682-local`
- 第三批：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/batch-3/20261007-154646-22850-local`
- 第四批：`/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/logs/acceptance/batch-4/20261007-154956-23761-local`

四个报告的 `summary.json` 均为 `"status":"passed"`。其中第一、第二、第三批在受限沙箱中执行时，涉及本地端口监听的测试会被环境拦截；已在允许本地回环监听的执行环境中重新复验通过。

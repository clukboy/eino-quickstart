# P02：API / worker 独立配置与验收准备

更新日期：2026-10-07。

> P02 的配置与验收基础设施已交付：独立文件、独立覆盖变量、按进程校验、隔离验收模板、依赖预检、编号入口、脱敏报告、唯一 fixture 和显式清理。当前工作区没有替你伪造外部依赖已就绪；真实 PostgreSQL / Redis / Milvus / embedding / worker 通过与否，必须在你的隔离环境执行 P02 后以报告为准。

## 1. 先弄清楚改哪个文件

| 进程 / 用途 | 默认文件 | 覆盖变量 | 修改内容 |
| --- | --- | --- | --- |
| API 业务 | `configs/api/config.yaml` | `EINO_API_CONFIG` | 对话、工具、权限、工作区、文档读写、检索、数据库和队列投递 |
| API HTTP | `configs/api/restapi.yaml` | `EINO_REST_CONFIG` | 实际 HTTP Host / Port、请求上限、超时、go-zero 日志与 tracing |
| 索引 worker | `configs/worker/config.yaml` | `EINO_WORKER_CONFIG` | 数据库、旧正文目录、分块、embedding、Milvus / ES、队列消费与日志 |
| 验收 API 业务 | `configs/acceptance/api.yaml` | `EINO_API_CONFIG` | 与 API 默认文件同结构，使用独立验收资源 |
| 验收 API HTTP | `configs/acceptance/restapi.yaml` | `EINO_REST_CONFIG` | 本机 `127.0.0.1:8090` |
| 验收 worker | `configs/acceptance/worker.yaml` | `EINO_WORKER_CONFIG` | 与 worker 默认文件同结构，使用独立验收资源 |
| 共用索引定义 | `configs/es/chunk_mapping.yaml` | 两份业务配置的 `ES.MappingFile` | 字段与检索权重定义，不是进程启动配置 |

**第一步建议：只打开两份 `configs/acceptance/*.yaml` 业务模板，先填数据库、Redis、Milvus 和 embedding，不要从 Agent 代码切入。**

- worker 不再读取 API 的 `Model` / `Auth` / `Security` / `Runtime` / `Workspace` 等配置，也不创建工作区。
- API 不再配置 `Indexer`、worker 并发数、消费队列权重、消费重试间隔和 worker 关闭超时。
- API 创建纯投递客户端，不再构造未启动的 asynq 消费服务。
- 不允许把另一进程专属字段混进文件；拼错字段也会在配置加载时失败（键名约定见 1.1）。
- 两个进程各用自己的 `Observability.ServiceName`，并分别写 `server.json` / `worker.json`。
- 文件路径和文件内的相对目录都按**进程工作目录**解释；以下命令要求从仓库根执行。

### 1.1 键名怎么写：大驼峰 = json tag 名

配置文件里的键名就是 `internal/platform/config` 结构体上的 `json` tag 名，也就是大驼峰字段名：

```yaml
Knowledge:
  MaxDocumentBytes: 5242880
  MaxChunksPerDocument: 2000
```

三条必须记住的规则：

1. **拼写必须一致，大小写不敏感**。go-zero 的解析把键名和 tag 名都转小写后精确比较：`MaxDocumentBytes` 和 `MAXDOCUMENTBYTES` 都能绑上，`max_document_bytes`、`maxDocumentBytes` 绑不上。
2. **绑不上不会报错**（go-zero 会静默忽略不认识的键，那一项保持零值），所以加载前会按 tag 树逐个键校验：未知键直接让进程启动失败，并尽量给出 `did you mean "MaxDocumentBytes"?`。这条校验同时适用于 `config.Load`（历史完整配置）。
3. **文件扩展名决定解析器**：`.yaml` / `.yml` / `.json` 都可以，共用同一套结构体与同一套键名（`EINO_API_CONFIG=/path/config.json` 是合法的）。

例外：`configs/es/chunk_mapping.yaml` 是索引映射声明，由 `internal/platform/storage/es` 单独加载，键名是它自己的 schema（`name` / `from` / `boost` …），不适用上面的约定；`configs/observability/otel-collector.yaml`、`configs/acceptance/compose.yaml` 分别是 OTel collector 与 Docker Compose 的配置文件，格式由它们自己规定。

## 2. 旧配置怎么处理

`configs/config.yaml` 和 `internal/transport/restapi/etc/restapi.yaml` 保留为历史文件，API / worker 不再默认读取。不要继续修改旧文件期待新入口生效。

API / worker（以及评测工具的默认选择）**不再读取 `EINO_CONFIG`，也不回退到它**：这是为了防止终端里残留的旧变量把两个进程重新指向同一份文件。已有启动脚本 / IDE 启动配置需要改成 `EINO_API_CONFIG` 和 `EINO_WORKER_CONFIG`；API HTTP 仍使用 `EINO_REST_CONFIG`。

调用旧 `config.Load` 的代码仍支持历史完整配置。新的入口使用 `config.LoadAPI` / `config.LoadWorker`，直接传入历史混合配置会报错，请按新模板迁移。

`cmd/rag-test` 默认使用 API 业务配置和 `EINO_API_CONFIG`；显式 `-config` 优先，但所选文件也必须是 API 配置结构。

## 3. 哪些值必须在两份文件里一致

文件独立不代表数据源独立。处理同一条文档链路的 API 和 worker 必须连接**同一组验收资源**：

| 参数 | 为什么要一致 | 验收模板默认值 |
| --- | --- | --- |
| `Storage.Host / Port / DBName` | API 写文档、worker 读同一篇文档 | `127.0.0.1:5432` / `eino_acceptance` |
| 数据库用户权限 | 两边都需要访问相关表；当前启动还会执行 schema 同步 | `postgres`，请按你的环境更换 |
| `Asynq.Redis.Addr / DB` | API 投递、worker 消费同一个 Redis 逻辑库 | `127.0.0.1:6379` / **15** |
| worker `Asynq.Queues` | 索引任务投递到契约固定的 `index` 队列 | `index`，权重 1 |
| `Milvus.Address / Collection / MetricType` | worker 写向量、API 检索或删除向量 | `127.0.0.1:19530` / `eino_acceptance_document_chunks_v1` / `COSINE` |
| `Embedding.BaseURL / Model / Dimensions` | 文档向量和查询向量必须属于同一向量空间 | 保留现有模型示例，维度 1024；上线前向服务商核实并实测 |
| `ES.Address / Index / MappingFile` | worker 写关键词索引，API 搜索同一索引 | 默认 `Address: []`，明确关闭 ES |
| `Knowledge.MaxDocumentBytes` | API 接受的正文应能由 worker 完整读取 | 5 MiB |

`Asynq.MaxRetries` 决定任务最大重试次数，API 投递值与 worker 的终态判断应保持一致。

ES 关闭时只是降级验证，不构成完整的关键词 / 精确召回验收。需要验证 ES 时，在**两份**模板同时填写独立服务地址、索引、分析器、证书和必要凭据；不要只开启 worker 或 API 一侧。

**安全边界：**

1. 默认开发文件保留之前的服务地址，仅用于兼容现有开发连接，不保证资源隔离。需要独立隔离时用 `configs/acceptance/`；已有部署时可用 `--existing` 直接复用默认业务配置。
2. 验收模板本身不会自动创建 PostgreSQL、Redis、Milvus 或 OTLP collector；可选的 `scripts/acceptance/dependencies.sh` 只负责启动本地 Docker 依赖，不改变模板的连接和验收规则。Redis DB 15 也必须确认专用、未被他人使用；Redis Cluster 不支持这种多逻辑库隔离，需要单独 Redis 实例并调整 DB。
3. 当前数据库启动路径含 schema 同步，可能修改表和索引。**不要用真实业务数据库试启动**，先创建独立 `eino_acceptance` 数据库。
4. 密钥全部走环境变量；文件中不填明文密钥，不提交本地凭据。
5. 验收模板的工作区是 `workspace/acceptance`，worker 旧正文目录为 `tests/knowledge/acceptance`；新文档正文存在数据库，不需要往旧目录放文件。

### 3.1 可选：用 Docker Desktop 启动本地依赖

这只是没有现成依赖时的备用方案，不是 P02 的前置条件。当前部署已有数据库和其他服务时，直接使用 `configs/api/config.yaml`、`configs/worker/config.yaml` 与 `configs/api/restapi.yaml`，不要启动本地 Docker；`configs/acceptance/` 只用于需要完全隔离的独立环境。

如果确实需要本地隔离依赖，才使用仓库内的可选 compose 栈。它只绑定回环地址，不启动 API / worker，也不提供 embedding、ES 或 OTLP collector；因此启动后仍需执行 P02 预检。

```bash
cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2
export EINO_STORAGE_PASSWORD='local-only-password'
bash scripts/acceptance/dependencies.sh up
bash scripts/acceptance/dependencies.sh status
```

这会启动项目名为 `eino-acceptance` 的 PostgreSQL、Redis、Milvus 及其 etcd / MinIO 支撑服务。PostgreSQL 密码来自 `EINO_STORAGE_PASSWORD`，应与 `configs/acceptance/api.yaml` 和 `configs/acceptance/worker.yaml` 的数据库凭据一致；不要把真实密码写进 `configs/acceptance/.env.example` 或提交到 Git。

常用操作：

```bash
# 查看最近日志（默认每个服务 100 行）
bash scripts/acceptance/dependencies.sh logs

# 停止容器，但保留 named volume，便于下一次继续使用
bash scripts/acceptance/dependencies.sh down
```

`down` 不删除卷；如果要重新初始化本地数据，必须由用户明确决定并单独执行受控的 compose 卷清理。脚本不会接管冲突端口，也不会操作其他 compose 项目。模板和脚本位置分别是 `configs/acceptance/compose.yaml`、`configs/acceptance/.env.example`、`scripts/acceptance/dependencies.sh`。

## 4. 不连接依赖，先验证“配置是否能加载”

### 4.1 一键运行配置回归测试

```bash
cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2
go test ./internal/platform/config ./internal/platform/queue/asynq -count=1
```

**通过标准：** 两个包显示 `ok`。测试使用临时文件和占位值，不连接 PostgreSQL、Redis、Milvus、ES 或模型。

覆盖：API / worker 及验收模板能加载、旧加载接口兼容、worker 不需要 API 环境变量、缺少 worker 必要字段失败、混用字段失败、拼错字段失败、独立覆盖变量互不影响、模板数据源一致、API 不创建消费者。

### 4.2 单独检查 worker

下面的假值**只适用于 `-check-config`**，不能用于真实启动。使用 `env` 只影响这一次命令，不污染后续终端环境。

```bash
env EINO_WORKER_CONFIG=./configs/acceptance/worker.yaml \
  EINO_STORAGE_PASSWORD=config-check-placeholder \
  EINO_EMBEDDING_API_KEY=config-check-placeholder \
  EINO_API_KEY_DEVELOPER= EINO_API_KEY_APPROVER= EINO_API_KEY_ADMIN= \
  EINO_ANONYMOUS_SECRET= EINO_ACCOUNT_SECRET= \
  go run ./cmd/worker -check-config
```

**通过标准：** 输出 `worker config OK: ./configs/acceptance/worker.yaml`，退出码为 0。即使不提供任何 API 身份或对话模型 key，也能完成 worker 配置检查。

### 4.3 单独检查 API

```bash
env EINO_API_CONFIG=./configs/acceptance/api.yaml \
  EINO_REST_CONFIG=./configs/acceptance/restapi.yaml \
  EINO_STORAGE_PASSWORD=config-check-placeholder \
  EINO_EMBEDDING_API_KEY=config-check-placeholder \
  EINO_API_KEY_DEVELOPER=config-check-placeholder \
  EINO_API_KEY_APPROVER=config-check-placeholder \
  EINO_API_KEY_ADMIN=config-check-placeholder \
  EINO_ANONYMOUS_SECRET=config-check-placeholder \
  EINO_ACCOUNT_SECRET=config-check-placeholder \
  go run ./cmd/restapi -check-config
```

**通过标准：** 输出 API 和 HTTP 两个 `config OK` 路径，退出码为 0。

这两个检查只做 YAML、环境变量存在性和已有配置规则校验：不监听端口、不建表、不连队列、不调用模型，不证明凭据有效、网络可达、向量维度匹配，也不证明指标监听已经开启。

### 4.4 验证“不能混用”

```bash
# 故意把 API 文件交给 worker；应该退出非 0，提示 API 专属 section 不允许。
EINO_WORKER_CONFIG=./configs/acceptance/api.yaml \
  go run ./cmd/worker -check-config
```

用模板副本测试，不改默认模板：在 worker 文件加入 `Model:`，或在 API 文件加入 `Indexer:`，也应该失败。修回原文件后再检查应恢复成功。

## 5. 接真实服务时，按这个顺序做

配置和预检入口已经交付；下面的真实服务启动、凭证有效性和 embedding 小调用使用已有部署配置执行，不需要本地 Docker。若目标服务尚未运行，P02 会生成 BLOCKED 报告，而不是假报通过。

### 5.1 先跑预检，再启动完整链路

从仓库根目录执行。预检只读取 API / worker / REST 配置；它不会建表、建 collection、建 ES index 或发送索引任务。Redis 权限探针使用本轮唯一 key，设置 1 秒过期并在结束时删除；embedding 只有显式加 `EINO_P02_PROBE_EMBEDDING=1` 才会发出一次可能计费的请求。

```bash
bash scripts/acceptance/run.sh --list

# 只允许 PostgreSQL 关键词回落的模式；ES address 为空是合法的。
# 复用已有部署，不启动本地 Docker。
EINO_STORAGE_PASSWORD='你的数据库密码' \
EINO_EMBEDDING_API_KEY='你的 embedding key' \
EINO_API_KEY_DEVELOPER='你的 agent key' \
EINO_API_KEY_APPROVER='你的 approver key' \
EINO_API_KEY_ADMIN='你的 admin key' \
EINO_ANONYMOUS_SECRET='独立匿名签名密钥' \
EINO_ACCOUNT_SECRET='独立账号签名密钥' \
bash scripts/acceptance/run.sh P02 --existing --degraded

# 确认 embedding 模型与维度时，显式允许一次小调用；不要无预算重复执行。
EINO_P02_PROBE_EMBEDDING=1 \
EINO_STORAGE_PASSWORD='你的隔离库密码' EINO_EMBEDDING_API_KEY='你的 embedding key' \
EINO_API_KEY_DEVELOPER='你的 agent key' EINO_API_KEY_APPROVER='你的 approver key' \
EINO_API_KEY_ADMIN='你的 admin key' EINO_ANONYMOUS_SECRET='独立匿名签名密钥' \
EINO_ACCOUNT_SECRET='独立账号签名密钥' \
bash scripts/acceptance/run.sh P02 --existing --degraded

# 只有配置了独立 ES 地址后才执行完整检索模式。
bash scripts/acceptance/run.sh P02 --existing --complete
```

报告目录会打印在命令末尾，里面有 `preflight.json`、`summary.json`、`preflight.log` 和带唯一标记的 `fixture.md`。报告只记录资源地址 / 名称和检查结果，不记录密码、API key 或签名密钥。`P02 --complete` 要求 ES 可用；`P02 --degraded` 允许 ES 关闭，但不会放宽 PostgreSQL、Redis、Milvus、worker 注册和 embedding 维度检查。

清理只接受一个 P02 报告目录，并且必须显式确认；不会执行 `FLUSHDB`、全库删除或删除共享 collection：

```bash
bash scripts/acceptance/run.sh P02-cleanup \
  /absolute/path/to/logs/acceptance/P02/<本轮报告目录名> --confirm
```

1. 先准备独立 PostgreSQL 数据库、专用 Redis DB / 实例、Milvus；需要 ES 完整验证时再准备独立索引。把实际地址写入两份验收业务配置。
2. 确认模型 / embedding 服务能使用配置中的模型名和维度；实际小调用可能计费，先确认预算。
3. 两个终端分别设置真实凭据，不复制第 4 节的占位值。worker 仅需要 `EINO_STORAGE_PASSWORD`、`EINO_EMBEDDING_API_KEY`，以及启用的 Redis / ES 凭据；API 还需要对话模型、配置所需的 API 身份凭据和令牌签名密钥。
4. 先各跑一次 `-check-config`，失败就修明确提示的文件 / 环境变量。通过后再去掉该参数。
5. **worker 终端：**

   ```bash
   export EINO_WORKER_CONFIG="$PWD/configs/acceptance/worker.yaml"
   go run ./cmd/worker
   ```

6. **API 终端：**

   ```bash
   export EINO_API_CONFIG="$PWD/configs/acceptance/api.yaml"
   export EINO_REST_CONFIG="$PWD/configs/acceptance/restapi.yaml"
   go run ./cmd/restapi
   ```

7. 在第三个终端检查：

   ```bash
   curl --fail --show-error http://127.0.0.1:8090/health
   curl --fail --show-error http://127.0.0.1:8090/ready
   ```

   `/ready` 主要探测 PostgreSQL，不代表队列、Milvus、ES 和模型都可用。继续按 `docs/user-acceptance-plan.md` 的 P04 创建小文档，观察 `indexing → ready` 并搜索唯一词，才算跑通真实索引链路。

8. 核对日志文件分离：`logs/acceptance/server.json` 和 `logs/acceptance/worker.json`；核对 API 和 worker 日志记录的配置路径及服务名。OTLP collector 示例为 `127.0.0.1:4317`，没有准备时不要宣称 tracing 已验收。

**不要做：** 不运行 `FLUSHDB`、不删共享 collection、不执行全库清空。P02 只清理自己报告目录里的 fixture；文档、数据集、队列和索引资源的创建/删除仍必须由对应 P04/P05 验收台账和显式清理入口负责。

## 6. 完成范围与剩余计划

- [x] API 业务 / API HTTP / worker 独立默认文件。
- [x] API / worker 独立覆盖变量，无共享变量回退。
- [x] 角色专属校验、字段拼写校验、API 纯队列投递客户端。
- [x] worker 不依赖对话 / 身份 / HTTP / 工作区配置。
- [x] 隔离验收配置模板、资源命名、离线检查命令和启动步骤。
- [ ] 按实际服务地址部署隔离依赖并验证连接、权限、embedding 维度（需要用户环境复测）。
- [x] 编号验收执行器 `scripts/acceptance/run.sh`、`--list`、P02 `--degraded/--complete` 和脱敏预检报告。
- [x] 独立报告目录、资源摘要、唯一 fixture 及 `P02-cleanup ... --confirm` 安全清理实现。
- [ ] API + worker 真实服务启动与小文档端到端验收（P04 负责业务链路证据）。

P01 的 Prometheus 指标策略已独立完成：REST 配置显式启用 `127.0.0.1:9091/metrics`，P02 的 API / worker 配置拆分不改变该约定。

## 7. 本次验证记录

- `go test ./internal/platform/config ./internal/platform/queue/asynq ./cmd/restapi ./cmd/worker ./cmd/rag-test -count=1`：通过（worker 暂无独立单元测试，包编译通过）。
- 第 4.2 / 4.3 节的 `-check-config`：均成功，worker 在 API 凭据为空时通过。
- `go test ./cmd/acceptance-preflight`：通过，覆盖 BLOCKED/FAIL 状态优先级、fixture 唯一标记/权限及脱敏报告。
- `bash scripts/acceptance/run.sh --list`、shell 语法检查：通过。
- `bash scripts/acceptance/run.sh P02 --degraded`：按预期 BLOCKED（API health/ready 通过；本机 PostgreSQL、Redis、Milvus 未启动；报告和 fixture 已生成），退出码 2；随后 `P02-cleanup ... --confirm` 只删除该报告目录的 fixture。
- 历史 P01 指标失败已在 2026-10-07 修复；当前全量 `go test -count=1 ./...` 已退出码 0。启用/禁用测试和实际 agent 抓取测试均通过，详见 `docs/p01-metrics-acceptance.md`。
- 没有运行真实 worker 启动、上传文档或 embedding 计费调用；真实 schema 同步由后续 P04/用户隔离环境承担。

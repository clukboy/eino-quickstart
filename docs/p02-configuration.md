# P02：API / worker 独立配置与验收准备

更新日期：2026-10-06。

> 本次完成 P02 的**配置部分**：独立文件、独立覆盖变量、按进程校验、隔离验收模板和离线检查入口。没有部署依赖，也没有完成真实服务预检、编号执行器、验收报告及自动清理。不能把“配置检查通过”当作“索引链路已验收”。

## 1. 先弄清楚改哪个文件

| 进程 / 用途 | 默认文件 | 覆盖变量 | 修改内容 |
| --- | --- | --- | --- |
| API 业务 | `configs/api/config.yaml` | `EINO_API_CONFIG` | 对话、工具、权限、工作区、文档读写、检索、数据库和队列投递 |
| API HTTP | `configs/api/restapi.yaml` | `EINO_REST_CONFIG` | 实际 HTTP Host / Port、请求上限、超时、go-zero 日志与 tracing |
| 索引 worker | `configs/worker/config.yaml` | `EINO_WORKER_CONFIG` | 数据库、旧正文目录、分块、embedding、Milvus / ES、队列消费与日志 |
| 验收 API 业务 | `configs/acceptance/api.yaml` | `EINO_API_CONFIG` | 与 API 默认文件同结构，使用独立验收资源 |
| 验收 API HTTP | `configs/acceptance/restapi.yaml` | `EINO_REST_CONFIG` | 本机 `127.0.0.1:8090` |
| 验收 worker | `configs/acceptance/worker.yaml` | `EINO_WORKER_CONFIG` | 与 worker 默认文件同结构，使用独立验收资源 |
| 共用索引定义 | `configs/es/chunk_mapping.yaml` | 两份业务配置的 `es.mappingFile` | 字段与检索权重定义，不是进程启动配置 |

**第一步建议：只打开两份 `configs/acceptance/*.yaml` 业务模板，先填数据库、Redis、Milvus 和 embedding，不要从 Agent 代码切入。**

- worker 不再读取 API 的 `model` / `auth` / `security` / `runtime` / `workspace` 等配置，也不创建工作区。
- API 不再配置 `indexer`、worker 并发数、消费队列权重、消费重试间隔和 worker 关闭超时。
- API 创建纯投递客户端，不再构造未启动的 asynq 消费服务。
- 不允许把另一进程专属字段混进文件；拼错字段也会在配置加载时失败。
- 两个进程各用自己的 `observability.serviceName`，并分别写 `server.json` / `worker.json`。
- 文件路径和文件内的相对目录都按**进程工作目录**解释；以下命令要求从仓库根执行。

## 2. 旧配置怎么处理

`configs/config.yaml` 和 `internal/transport/restapi/etc/restapi.yaml` 保留为历史文件，API / worker 不再默认读取。不要继续修改旧文件期待新入口生效。

API / worker（以及评测工具的默认选择）**不再读取 `EINO_CONFIG`，也不回退到它**：这是为了防止终端里残留的旧变量把两个进程重新指向同一份文件。已有启动脚本 / IDE 启动配置需要改成 `EINO_API_CONFIG` 和 `EINO_WORKER_CONFIG`；API HTTP 仍使用 `EINO_REST_CONFIG`。

调用旧 `config.Load` 的代码仍支持历史完整配置。新的入口使用 `config.LoadAPI` / `config.LoadWorker`，直接传入历史混合配置会报错，请按新模板迁移。

`cmd/rag-test` 默认使用 API 业务配置和 `EINO_API_CONFIG`；显式 `-config` 优先，但所选文件也必须是 API 配置结构。

## 3. 哪些值必须在两份文件里一致

文件独立不代表数据源独立。处理同一条文档链路的 API 和 worker 必须连接**同一组验收资源**：

| 参数 | 为什么要一致 | 验收模板默认值 |
| --- | --- | --- |
| `storage.host / port / dbName` | API 写文档、worker 读同一篇文档 | `127.0.0.1:5432` / `eino_acceptance` |
| 数据库用户权限 | 两边都需要访问相关表；当前启动还会执行 schema 同步 | `postgres`，请按你的环境更换 |
| `asynq.redis.addr / db` | API 投递、worker 消费同一个 Redis 逻辑库 | `127.0.0.1:6379` / **15** |
| worker `asynq.queues` | 索引任务投递到契约固定的 `index` 队列 | `index`，权重 1 |
| `milvus.address / collection / metricType` | worker 写向量、API 检索或删除向量 | `127.0.0.1:19530` / `eino_acceptance_document_chunks_v1` / `COSINE` |
| `embedding.baseURL / model / dimensions` | 文档向量和查询向量必须属于同一向量空间 | 保留现有模型示例，维度 1024；上线前向服务商核实并实测 |
| `es.address / index / mappingFile` | worker 写关键词索引，API 搜索同一索引 | 默认 `address: []`，明确关闭 ES |
| `knowledge.maxDocumentBytes` | API 接受的正文应能由 worker 完整读取 | 5 MiB |

`asynq.maxRetries` 决定任务最大重试次数，API 投递值与 worker 的终态判断应保持一致。

ES 关闭时只是降级验证，不构成完整的关键词 / 精确召回验收。需要验证 ES 时，在**两份**模板同时填写独立服务地址、索引、分析器、证书和必要凭据；不要只开启 worker 或 API 一侧。

**安全边界：**

1. 默认开发文件保留之前的服务地址，仅用于兼容现有开发连接，不保证资源隔离。P02 验收请用 `configs/acceptance/`。
2. 验收模板是配置，不会自动创建 PostgreSQL、Redis、Milvus 或 OTLP collector。Redis DB 15 也必须确认专用、未被他人使用；Redis Cluster 不支持这种多逻辑库隔离，需要单独 Redis 实例并调整 DB。
3. 当前数据库启动路径含 schema 同步，可能修改表和索引。**不要用真实业务数据库试启动**，先创建独立 `eino_acceptance` 数据库。
4. 密钥全部走环境变量；文件中不填明文密钥，不提交本地凭据。
5. 验收模板的工作区是 `workspace/acceptance`，worker 旧正文目录为 `tests/knowledge/acceptance`；新文档正文存在数据库，不需要往旧目录放文件。

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

用模板副本测试，不改默认模板：在 worker 文件加入 `model:`，或在 API 文件加入 `indexer:`，也应该失败。修回原文件后再检查应恢复成功。

## 5. 接真实服务时，按这个顺序做

这部分是**用户下一步验证步骤，本次没有代你执行**。

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

**不要做：** 不运行 `FLUSHDB`、不删共享 collection、不执行全库清空。后续清理必须依据本轮资源台账；本次没有提供自动清理脚本。

## 6. 完成范围与剩余计划

- [x] API 业务 / API HTTP / worker 独立默认文件。
- [x] API / worker 独立覆盖变量，无共享变量回退。
- [x] 角色专属校验、字段拼写校验、API 纯队列投递客户端。
- [x] worker 不依赖对话 / 身份 / HTTP / 工作区配置。
- [x] 隔离验收配置模板、资源命名、离线检查命令和启动步骤。
- [ ] 按实际服务地址部署隔离依赖并验证连接、权限、embedding 维度。
- [ ] 编号验收执行器 `scripts/acceptance/run.sh`、`--list` 和预检。
- [ ] 独立报告目录、资源台账及安全清理实现。
- [ ] API + worker 真实服务启动与小文档端到端验收。

P01 的 Prometheus / DevServer 指标策略属于另一项。本次保留当前 HTTP 配置策略，不用 P02 的配置拆分掩盖已有指标测试问题。

## 7. 本次验证记录

- `go test ./internal/platform/config ./internal/platform/queue/asynq ./cmd/restapi ./cmd/worker ./cmd/rag-test -count=1`：通过（worker 暂无独立单元测试，包编译通过）。
- 第 4.2 / 4.3 节的 `-check-config`：均成功，worker 在 API 凭据为空时通过。
- `go test ./...`：未全绿；剩余失败在 `internal/transport/restapi` 的 `TestShippedConfigKeepsAgentsConfigurable` 和 `TestPrometheusAgentServesGoZeroMetrics`。当前 Prometheus 配置仍被注释，用户已有 DevServer 测试调整与配置尚未对齐，属于 P01 待处理事项，本次未改变指标策略。
- 没有运行真实服务启动、上传文档、模型调用或 schema 同步。

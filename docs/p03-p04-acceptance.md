# P03 / P04：契约与文档生命周期验收

交付日期：2026-10-06。本文是可执行的验收指南，不将模拟依赖测试冒充真实端到端结果。

## 1. 先看交付状态

| 项目 | 已完成 | 仍需用户环境验证 |
| --- | --- | --- |
| P03 文档/契约 | 修正正文、上传、异步状态、产品拆分边界；可选请求字段修正；Swagger 对齐；隔离生成和编译检查 | 按独立配置启动真实 API/worker，再跑 HTTP 样例 |
| P04 生命周期 | 真实应用服务 + Ent 内存数据库 + Markdown Pipeline 的生命周期回归；实际 HTTP handler 契约回归；真实 HTTP 执行器与资源台账 | 隔离 PostgreSQL/Redis/Milvus/embedding 的正常路径与故障演练；外部存储/SQL 核对 |

**本轮没有启动真实 API/worker，也没有往共享数据库写入验收数据。**
配置文件已有模板不代表依赖已经就绪。P01 的两个指标测试仍是独立待办，不在这里跳过或改成假通过。

## 2. 不知道切入点？先运行这两条

在仓库根目录执行：

```bash
export REPO="/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2"
cd "$REPO"

# 只列已实现的编号和模式。
bash "$REPO/scripts/acceptance/run.sh" --list

# P03：临时目录生成 types/routes/Swagger；比较并编译，绝不覆盖工作区。
bash "$REPO/scripts/acceptance/run.sh" P03

# P04：无需真实数据库/队列/模型密钥，完成本地生命周期和 HTTP 契约回归。
bash "$REPO/scripts/acceptance/run.sh" P04 --local
```

- P03 需要 Go、Python 3、`goctl`（本轮使用 `1.9.2`）。未自动安装或升级生成器。
- P04 本地测试需要 Go、Python 3 和可用的 C 编译器（SQLite 测试驱动使用 CGO）。
- 本地 embedding 使用临时监听端口；受限沙箱需允许监听和 Go 构建缓存。
- 成功看到 `PASS`；日志保存在 `logs/acceptance/P03/<run-id>/check.log` 或
  `logs/acceptance/P04/<run-id>-local/tests.log`。本地模式的 PASS **不是**真实依赖验收。
- 也可执行 `make check-api` / `make test-knowledge-lifecycle`。

### 本地 P04 实际断言了什么

| 场景 | 检查内容 |
| --- | --- |
| 创建、暂停消费 | 一条文档、正文已入库、任务排队、`indexing`、未切块、尚不可检索 |
| 处理完成 | `ready`，`chunk_count > 0`，全部 chunk indexed，正文逐字一致，测试词召回目标文档 |
| 重复 catch-up 任务 | chunk ID 不变、唯一分块序号不增生、向量/关键词条数与 chunk 一致 |
| 更新 A → B | 正文 B 持久化、B 可召回、旧 A 不在实际 chunk 或关键词存储中 |
| 单文档 / 整库重建 | 明确 rebuild，而非 catch-up 空转；替换分块后两篇文档恢复 ready |
| 删除及滞后任务 | 删除后详情不可读、搜索不返回目标 ID；删除前已排队的任务不会复活文档 |
| 投递故障 | 应用返回 `ErrQueueUnavailable`，HTTP 返回 503，已落库文档可见且 failed；恢复后 reindex 成功 |
| 向量 / 关键词写失败 | 保持待续跑状态；依赖恢复后按原 chunk ID 幂等 upsert |
| 部分批次成功 | 仅续跑未 indexed 的批次，已成功批次不重复 embedding；外部写入不增生 |
| 重试耗尽 | 向量/关键词存储阶段和分块事务阶段均落 failed；修复后显式 reindex 可恢复 |
| 进程取消 | 即使处于最后重试也不误落 failed，可由后续消费者继续 |
| HTTP 请求解析 | 标题+正文即可创建；可选字段省略可用；错误字段类型/空正文/空查询为 400；multipart 文件字段 `file` 可重复 |
| 正文读取错误 | 不存在及已删除文档的详情和 `/content` 均为 404 + `not_found` |

测试使用真实 Service、Indexer、Ent schema/事务和 Markdown 分块逻辑。
队列、向量/关键词端口、召回候选、embedding 响应可控替换；**不覆盖真实 Asynq 调度、
PostgreSQL 方言差异、Milvus/ES 可用性或语义检索质量**。

## 3. P03：这次纠正了哪些地方

| 原先容易误读的说法/契约 | 现在的准确信息 |
| --- | --- |
| `source` 可以代替正文，注册磁盘文件 | 新写入必须给非空 `content`；`source` 是逻辑标识，不会读取文件 |
| 新正文写托管目录 | 正文唯一真相为 `documents.content`；旧目录只供存量空正文一次性导入 |
| worker 将一份型录拆成多条文档 | 产品型录未指定显式 source 且可解析产品块时，在 API 请求内拆成多条；worker 切的是每条文档的 chunk |
| 初次 chunk_count 是最终数量 | 实时聚合，不是最终承诺；新建通常为 0，并发 worker 可能已推进，更新/重建可暂存旧计数 |
| `omitempty` 就能让请求字段选填 | go-zero 绑定用 `optional`；创建的 source/visibility/metadata、更新字段、top_k 与上传 visibility 已改正 |
| 不支持 multipart | 已支持 `POST .../documents/upload`，字段名 `file`，可重复；仍非无限容量流式导入 |
| 不存在正文响应是一般 bad_request | `/content` 和详情同样走领域错误映射，返回明确的 404 |
| goctl 不覆盖任何文件 | types/routes/Swagger 会覆盖；已有 handler/logic 不覆盖，签名变更需手动适配 |
| Swagger 上传是 urlencoded 且缺 file | 声明式扩展补上 multipart、必填 file 和多文件说明 |
| Swagger 把 omitempty 响应字段标为必填 | 扩展按生成 Go 类型的 JSON 形状纠正，保留真正必填字段 |

### 生成来源与兼容性

1. 类型和路由来源：`internal/transport/restapi/docs/restapi.api` 及其 import 子文件。
2. Swagger 扩展来源：`internal/transport/restapi/docs/swagger-overrides.json`；
   `scripts/normalize-swagger.py` 将 goctl 未表达的 multipart 文件、HTTP/HTTPS、错误响应
   及 `omitempty` 响应语义确定性合入 Swagger。
3. `make gen-api` 和 `make check-api` 使用**同一条扩展步骤**。不要只运行裸 Swagger 生成后
   把缺失 multipart 的产物当作最终契约；也不要手改生成的 JSON。
4. `make check-api` 在镜像目录复制现有业务文件后生成，比对 types/routes/Swagger，
   仅忽略 Swagger `x-date` 时间戳，不忽略接口、参数或 schema；再以 Go overlay 编译
   当前手写 handler/logic。生成漂移时返回非 0 且显示差异。
5. 本轮保留了用户已经补齐、且 `.api` 已声明的主体/分页六组类型；routes 无需改动。
   未使用 `regen-api`，未删除 handler，未引入新的 TypeScript 生成物（本仓库当前无该目录）。
6. 路径、HTTP 方法、响应字段命名和成功 200 保持不变；仅放宽真正选填的请求字段。
   请求字段类型仍严格解析；正文不能只给 source；不存在正文由模糊的 400 修正为 404。
   更新正文空字符串仍表示“不改”，不是清空正文接口。
7. `DocumentResp` 不带完整 content，完整正文由 `/content` 返回；创建/上传返回 `{"data":[...]}`。
   `operation` 仅写响应有值，读响应允许不出现；`content_truncated` 为 true 时表示检索内容被截断。

## 4. P04：运行真实 HTTP 验收

### 4.1 先确认配置和隔离资源

先按 [P02 配置指南](p02-configuration.md) 准备**真正的专用资源**。
不要仅复制模板就认定已隔离；API 启动可能同步数据库结构，禁止指向共享/生产库。

```bash
export EINO_API_CONFIG="$REPO/configs/acceptance/api.yaml"
export EINO_WORKER_CONFIG="$REPO/configs/acceptance/worker.yaml"
export EINO_REST_CONFIG="$REPO/configs/acceptance/restapi.yaml"
# 密钥只在终端环境设置，不写 Markdown、命令参数或报告。
# EINO_STORAGE_PASSWORD / EINO_REDIS_PASSWORD / EINO_EMBEDDING_API_KEY 等按实际配置设置。

go run ./cmd/restapi -check-config
go run ./cmd/worker -check-config
```

确认两份业务配置连接的是同一个专用 PG 库、Redis DB、Milvus collection 和 embedding 维度；
ES 配置为空可走 PostgreSQL 关键词降级，不代表 BM25 已验证。需要模型/embedding 的真实
小调用时先确认调用预算。两个终端分别启动：

```bash
# 终端 A，已导出 worker 所需变量
go run ./cmd/worker

# 终端 B，已导出 API 所需变量
go run ./cmd/restapi
```

### 4.2 一条命令跑正常路径

```bash
# 终端 C；使用已经获授权的专用管理员凭据。不要把实际值粘进文档。
read -s EINO_ACCEPTANCE_TOKEN
export EINO_ACCEPTANCE_TOKEN
export EINO_ACCEPTANCE_BASE="http://127.0.0.1:8090/api/v1"

# 人工核对资源清单之后才设为 1；脚本无法从 HTTP 证明后端资源是否隔离。
export EINO_ACCEPTANCE_ISOLATED=1
bash "$REPO/scripts/acceptance/run.sh" P04 --timeout 120
```

脚本会创建唯一 `acceptance-p04-<随机值>` 的普通数据集，依次执行：

1. 创建 A 文档，记录 ID；轮询 ready 和计数。
2. `/content` 校验全文 A；搜索 A 必须返回目标 ID 和测试词。
3. 空正文/空查询必须返回 400 + `bad_request`。
4. 将正文更新成 B；重新等待，全文 B 相等，目标检索内容不残留 A。
5. 单文档 reindex；轮询完成并重新搜索 B。
6. multipart 上传第二篇 C，检查文件字段、列表响应和正文。
7. 整库 reindex：`status=accepted`、`documents=2`、`failed=0`、兼容字段 `chunks=0`；两篇恢复 ready。
8. 删除 A；详情和正文 404，搜索不得再返回 A 的 ID。

默认不清库、也不删除第二篇 C 和数据集，用户可直接在 API/前端查询剩余资源。
**正常流程会删除第一篇，这是生命周期删除测试本身。** 所有创建/删除 ID 均有台账。

### 4.3 报告怎么看

`logs/acceptance/P04/<run-id>/`：

- `summary.json`：`PASS` / `FAIL` / `BLOCKED`。
- `resources.json`：本轮数据集名/ID、文档 ID、已删除 ID 和通过的检查点。
- `0001-http.json` 等：请求方法/路径、固定测试正文、状态码、响应、耗时。

凭据和 Authorization 头不写入报告，也不跟随 HTTP 重定向。
真实模式退出码 0=通过，1=断言失败，2=缺少凭据/隔离确认/网络等前置条件。
单阶段默认最多等待 120 秒（单请求网络超时 10 秒）；超时不会无限轮询或标成 PASS。
本地模式只报告本地回归，不能替代这里的 live 报告。

### 4.4 清理只针对本轮资源

```bash
# 想验证完即清理时，开始这一轮就带 --cleanup。
bash "$REPO/scripts/acceptance/run.sh" P04 --timeout 120 --cleanup
```

清理前复核数据集 ID + 唯一名，逐个删除台账中的文档，再确认库内没有不认识的文档，
最后删除本轮数据集。没有执行 SQL 清库、删共享索引或 flush Redis。
失败时也只尝试清理已经记录的 ID，清理不成功会保留报告并返回失败。
如果网络断开导致服务已创建资源但未返回 ID，不能猜 ID 删除：按唯一数据集名称人工核对。
不带 `--cleanup` 的历史报告需按 resources.json 手工操作，不会被下一轮批量清掉。

## 5. 故障演练：按这个顺序操作

**只能针对专用验收服务。不要停止共享 Redis，也不要让脚本 kill 任意进程。**

### 5.1 worker 暂停与恢复

1. 在终端 A 只停止当前专用 worker（正常 Ctrl-C），保持 API 和依赖运行。
2. 终端 C 执行：

   ```bash
   bash "$REPO/scripts/acceptance/run.sh" P04 --expect-paused-worker --timeout 120 --cleanup
   ```

3. 脚本创建文档后连续五秒要求 `indexing`、`chunk_count=0`，不会把排队当完成。
4. 看到“现在请启动隔离 worker”时，在终端 A 重启 worker。
5. 后续 ready / 正文 / 搜索 / 更新 / 重建 / 删除必须全部通过；超过等待时间则失败。

### 5.2 队列发布失败与恢复

1. API 和专用数据库保持运行，仅令**专用验收 Redis**不可用；不要重新启动 API
   去连接坏依赖（否则测到的是启动失败，不是已运行 API 的投递失败）。
2. 运行：

   ```bash
   bash "$REPO/scripts/acceptance/run.sh" P04 --expect-queue-outage --timeout 120 --cleanup
   ```

3. 写请求必须 503 + `service_unavailable`；列表中这条已保存文档必须 `failed`。
4. 看到恢复提示后，恢复专用 Redis 和 worker。脚本有界尝试显式 reindex，不会重复 create。
5. 任务重新受理后必须收敛到 ready，继续完成正常生命周期。

### 5.3 索引任务失败、重试耗尽与修复

本地回归已经自动注入向量失败、关键词写失败、分块事务失败、部分批次成功与取消。
真实环境手工演练时：

1. 使用独立验收 worker/embedding 环境，令指定索引依赖临时失败，再创建小文档。
2. 记录 document ID 和 worker 原始错误；未耗尽时可继续 indexing，耗尽后必须 failed。
   演练前将专用配置的 `asynq.maxRetries` 与重试间隔调到有限可观察值，不改共享配置。
3. 恢复依赖；`POST /dataset/<ID>/documents/<DOC_ID>/reindex` 返回受理状态。
4. 有界等待 ready，核对正文字节、实际分块序号与外部索引数量。
5. 进程正常退出产生的 context cancellation 不应误判“重试耗尽的业务失败”。

## 6. 真实存储的补充核对（不能只看搜索一句话）

本地回归已对真实 Ent chunk 行检查 ID、序号、内容和可控索引条目。
真实验收还应在**专用 PostgreSQL**执行只读 SQL（将变量换成本轮台账 ID）：

```sql
-- 更新 B 并 ready 后；计数需一致、旧 A 分块数需为 0。
SELECT d.id, d.status, octet_length(d.content) AS body_bytes,
       count(c.id) AS chunk_count,
       count(c.id) FILTER (WHERE c.vector_status = 'indexed') AS indexed_chunk_count,
       count(c.id) FILTER (WHERE c.content LIKE '%验收蓝鲸词A%') AS stale_a_chunks
FROM documents d LEFT JOIN document_chunks c ON c.document_id = d.id
WHERE d.id = <DOC_ID>
GROUP BY d.id, d.status, d.content;

-- catch-up 重试前后，已有 chunk ID/序号应一致，不出现重复序号。
SELECT id, chunk_index, vector_status
FROM document_chunks WHERE document_id = <DOC_ID> ORDER BY chunk_index;

-- 删除后应无此文档对应的 chunk 行。
SELECT count(*) FROM document_chunks WHERE document_id = <DELETED_DOC_ID>;
```

显式 reindex 会重新生成 chunk ID，不能把这当成幂等失败；普通失败重试才应复用 ID。
按这些 ID 核对本轮专用 Milvus/ES，不用全库总数来混淆他人数据。
旧词的语义查询可能仍召回新文档，**不能**用“旧查询必须零结果”证明更新成功。
使用实际 chunk 内容、外部条目 ID 和目标 document_id 作为证据。

## 7. 本轮验证记录

| 检查 | 实际结果 | 本轮报告（仓库根目录下） |
| --- | --- | --- |
| P03 临时生成、差异检查、编译及 Swagger 回归 | PASS | `logs/acceptance/P03/20261006-162749-82831/check.log` |
| P04 本地回归（8 个生命周期测试、HTTP handler 契约、11 个 Python 测试） | PASS | `logs/acceptance/P04/20261006-162752-82886-local/tests.log` |
| Go 生命周期及 HTTP handler 竞态检测 | PASS | `logs/acceptance/regression/lifecycle-race.log` |
| 全量 `go test -count=1 ./...` | FAIL：仅原有 2 项 P01 指标测试 | `logs/acceptance/regression/full-go-test.log`、`full-go-test-exit.txt` |
| 缺少隔离确认的真实模式前置检查 | BLOCKED，退出码 2；未发 HTTP 请求 | `logs/acceptance/P04/20261006-162820-1ead2eb8/summary.json` |
| 不支持的 P99 编号 | BLOCKED，退出码 2 | 终端检查；未连接任何依赖 |
| 真实 HTTP E2E / 外部存储核对 | **未执行** | 等待隔离资源与有效凭据 |

原有失败为 `TestShippedConfigKeepsAgentsConfigurable` 与
`TestPrometheusAgentServesGoZeroMetrics`；本轮没有跳过测试或修改 P01 指标配置。
Python 测试只验证执行器断言、清理边界及 Swagger 扩展，未对真实数据库执行清理。
报告可能含本轮测试正文，应在分享前检查；它们默认在忽略的 `logs/` 下，不自动提交。
因此不能因本地 P03/P04 通过，宣称全仓库全绿或真实依赖验收已通过。

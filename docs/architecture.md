# 架构与运行说明

## 分层

项目按职责分为四层，依赖方向应从入口和应用层指向平台能力，而不是反向依赖。

| 层 | 路径 | 职责 |
| --- | --- | --- |
| 传输层 | `internal/transport/restapi` | HTTP 路由、认证入口、SSE 响应和请求编排 |
| 应用层 | `internal/application` | Agent 组装、上下文管理、工具中间件、知识库用例（`knowledge`） |
| 领域能力 | `internal/rag`、`internal/tool`、`internal/skill` | 正文读写、解析/切块/向量存储、工具实现与 Skill 加载 |
| 平台层 | `internal/platform` | 配置、认证、执行器、可观测性、隐私、队列与持久化 |

依赖方向是单向的：传输层 → 应用层 → 领域能力/平台层。应用层对平台能力的引用走
**接口**（port），实现放在平台层并由组合根注入 —— 例如
`application/knowledge.IndexTaskQueue` 由 `platform/queue/asynq.IndexQueue` 实现，
所以适配器不需要反向 import 应用层。

进程有两个组合根，各自装配自己需要的那一半依赖：

| 组合根 | 装配 |
| --- | --- |
| `cmd/restapi` | 配置、ent、工具注册表、Agent Harness、知识库用例（含**投递口**）、HTTP 服务 |
| `cmd/worker` | 配置、ent、embedding、Milvus、Elasticsearch、`knowledge.Indexer`、asynq **消费端** |

`cmd/ragserver` 是本地跑 RAG 链路用的调试入口，不属于生产运行形态。

## Agent 结构

```text
HTTP /api/v1/chat
        |
   root_agent
  /     |      \
knowledge workspace automation
```

| Agent | 可用能力 | 边界 |
| --- | --- | --- |
| `root_agent` | 调用专项 Agent | 只路由，不直接读取、写入或执行 |
| `knowledge_agent` | `search_knowledge` | 仅返回授权知识库内容和引用 |
| `workspace_agent` | `read_file`、`list_dir` | 只读工作区 |
| `automation_agent` | `write_file`、可选 `shell` | 受工具白名单和审批策略控制 |

专项 Agent 定义于 `internal/application/agent`。根 Agent 使用 `adk.NewAgentTool` 包装专项 Agent；工具注册表位于 `internal/tool/registry`，工具本身必须先注册、再显式授予目标 Agent。

## 请求生命周期

1. `POST /api/v1/chat` 经过 API Key 认证，创建或恢复会话。
2. 服务写入用户消息和 `ChatTurn`，准备有限长度的历史上下文。
3. Eino Runner 流式运行根 Agent；响应使用 SSE 返回内容、审批请求或错误事件。
4. 工具调用经过 `middleware.Policy`：不在 `security.allowedTools` 的工具被拒绝；受保护写入或 shell 操作创建审批并中断运行。
5. 审批人调用 decision API 后，调用 resume API 恢复检查点；原始参数哈希必须匹配，防止审批后替换操作。
6. 回答完成后，服务保存助手消息并关闭本轮运行状态。

## 配置与环境变量

默认配置文件为 `configs/config.yaml`，可通过 `EINO_CONFIG` 覆盖。`cmd/restapi` 还会
额外读一份传输层配置 `internal/transport/restapi/etc/restapi.yaml`（`EINO_REST_CONFIG`
可覆盖），里面只放监听地址、请求体上限、超时和日志；业务配置一律走前者。支持以下
覆盖项：

| 环境变量 | 用途 |
| --- | --- |
| `EINO_MODEL_API_KEY` | 对话模型 API Key |
| `EINO_MODEL_BASE_URL` | 对话模型 Base URL |
| `EINO_MODEL` | 对话模型名称 |
| `EINO_SERVER_PORT` | HTTP 端口 |
| `EINO_WORKSPACE_ROOT` | 工作区根目录 |
| `EINO_EMBEDDING_API_KEY` | Embedding API Key，名称由 `embedding.apiKeyEnv` 配置；只有 `cmd/worker` 需要 |
| `EINO_STORAGE_PASSWORD` | PostgreSQL 密码，名称由 `storage.passwordEnv` 配置 |
| `EINO_ES_PASSWORD` | Elasticsearch 密码，名称由 `es.passwordEnv` 配置；ES 未配置时不需要 |
| `EINO_API_KEY_DEVELOPER` | Agent 调用 API Key |
| `EINO_API_KEY_APPROVER` | 审批 API Key |
| `EINO_API_KEY_ADMIN` | 管理 API Key |

Redis 口令同理走 `asynq.redis.passwordEnv`，两个进程都要能读到同一个值。

不要将 API Key、数据库密码或生产地址提交到配置文件。`storage.passwordEnv` 必须指向一个已设置的环境变量。

## HTTP API

| 方法 | 路径 | 角色 | 用途 |
| --- | --- | --- | --- |
| `GET` | `/health` | 无 | 存活检查 |
| `GET` | `/ready` | 无 | 数据库就绪检查 |
| `POST` | `/api/v1/sessions` | `agent` | 创建会话 |
| `POST` | `/api/v1/chat` | `agent` | 发起流式对话（SSE） |
| `GET` | `/api/v1/approvals/{id}` | `approver` | 查询审批 |
| `POST` | `/api/v1/approvals/{id}/decision` | `approver` | 批准或拒绝 |
| `POST` | `/api/v1/approvals/{id}/resume` | `agent` | 恢复已批准运行（SSE） |
| `POST` | `/api/v1/dataset` | `admin` | 创建数据集；`name`、可选 `description` 和 `visibility`（`private` 或 `system`） |
| `GET` | `/api/v1/dataset` | `admin` | 列出数据集 |
| `GET` | `/api/v1/dataset/{id}` | `admin` | 获取数据集详情 |
| `DELETE` | `/api/v1/dataset/{id}` | `admin` | 删除数据集；成功返回 `{"status":"deleted"}` |
| `POST` | `/api/v1/dataset/{id}/documents` | `admin` | 创建文档；`content` 写进 `documents.content`；产品型录会按产品块拆成多条。返回 `status: indexing` |
| `GET` | `/api/v1/dataset/{id}/documents` | `admin` | 列出数据集下的文档，含分块数与已索引分块数 |
| `GET` | `/api/v1/dataset/{id}/documents/{docId}` | `admin` | 获取文档详情 |
| `PUT` | `/api/v1/dataset/{id}/documents/{docId}` | `admin` | 更新文档；给 `content` 会改写正文并重新索引 |
| `DELETE` | `/api/v1/dataset/{id}/documents/{docId}` | `admin` | 删除文档，一并清理分块、索引任务与向量；返回 `{"status":"deleted"}` |
| `POST` | `/api/v1/dataset/{id}/documents/{docId}/reindex` | `admin` | 重建单个文档索引 |
| `POST` | `/api/v1/dataset/{id}/reindex` | `admin` | 重建数据集全部文档索引；返回受理计数，不等待索引完成 |
| `GET` | `/api/v1/agents/{subject}/dataset` | `admin` | 查询 API Key 主体可访问的数据集 |
| `PUT` | `/api/v1/agents/{subject}/dataset/{id}` | `admin` | 为 API Key 主体授权一个启用中的数据集；成功与重复授权都是空体 204 |
| `DELETE` | `/api/v1/agents/{subject}/dataset/{id}` | `admin` | 撤销授权；成功返回空体 204 |

文档的写操作**不同步等索引**，而且请求内**不切块**。HTTP 侧只做三件事：

1. 把正文写进 `documents.content`；
2. 写下 `documents` 行，`status=indexing`；
3. 把「这篇文档要索引」投进 Redis 队列（`knowledge:index` / 队列 `index`）。

切块、embedding 和写向量全部在 `cmd/worker` 进程里完成。所以响应返回时 `status`
是 `indexing`，而 `chunk_count` 与 `indexed_chunk_count` 都是**实时聚合值**（从
`document_chunks` 现算，`documents` 表不存冗余计数）：

- 刚创建时两者都是 `0`，不代表文档为空 —— 只是 worker 还没跑到。客户端不要用
  `chunk_count == 0` 判空文档；
- `chunk_count` 由 worker 切块时逐批写入而增长；`chunk_count` 追平且该文档所有
  chunk 都是 `indexed` 后，`status` 变 `ready`；
- 重试耗尽仍失败的 chunk 会把文档置为 `failed`。所以 `status != ready` 也不等于
  失败，要看具体是 `indexing` 还是 `failed`。

投递失败是**同步失败**：队列不可用（Redis 连不上、`asynq.enabled=false`）时请求直接
返回错误，`documents` 行被标记为 `failed`，不会留下一个永远停在 `indexing` 的孤儿。

路由与类型由 `internal/transport/restapi/docs/*.api` 驱动 goctl 生成，是契约的唯一来源。
指标不再暴露在同端口：go-zero 自带 Prometheus agent 在
`internal/transport/restapi/etc/restapi.yaml` 的 `Prometheus.Host/Port` 上独立监听
（当前该段是注释状态，即指标未暴露）。

## 知识库数据流

摄取与索引跨两个进程，唯一的连接是 Redis 队列和 `platform/queue/tasks` 里的任务契约：

```text
【cmd/restapi · HTTP 进程】只做「接收 + 落库 + 投递」，不碰 embedding 和向量库
  上传正文 → 拆产品块（YAML 头进 documents.metadata，正文进 documents.content）
           → INSERT documents (status=indexing)
           → Enqueue(knowledge:index, {dataset_id, document_id, mode})
                    │
      ══════════════╎══════════════  Redis 队列 index（asynq broker）
                    │              payload: 信封{v, trace, body=KnowledgeIndexPayload}
                    │              trace 里是 W3C traceparent —— 见「链路追踪」
【cmd/worker · 消费进程】承担全部重活，PostgreSQL / Milvus / ES / embedding 只在这里构造
  HandleTask → 从 documents.content 取正文（空则从旧文件导入一次，见下文）
             → rag.Pipeline.IngestContent（与 cmd/ragserver 同一份切块实现）
                 TextParser → MarkdownChunker 切块
             → INSERT document_chunks (vector_status=pending)
             → Embedder 批量向量化 → Milvus upsert
             → 写检索索引 ES（_id = chunk_id；未配 es.address 时跳过）
             → chunk: pending→indexed；全部追平后 document: indexing→ready
             → 重试耗尽仍失败 → chunk: failed，document: failed

【查询】也在 HTTP 进程内（未拆分）
  三通道并行：精确（型号/系列等结构化字段逐字相等，ES term）
            + 关键字（配了 ES 走 BM25；没配回落 PostgreSQL 词元子串匹配）
            + 向量（Milvus 取 chunk_id → 回 PostgreSQL 补齐正文与 provenance）
    → 加权 RRF 融合 → ACL 过滤 → 检索结果
```

> 三个通道都由 `rag.Store` 提供，`rag.HybridRetriever` 负责融合，在 HTTP 进程内同步
> 执行。**任一通道失败只降级不整体失败**（三条全失败才报 `ErrSearchUnavailable`），
> 降级情况通过响应的 `degraded` 字段暴露给调用方 —— 这样「通道挂了」与「确实没有
> 相关内容」在客户端可以被区分开，而不是都表现为空结果。
>
> 通道的取舍是刻意的：**精确与关键字通道只依赖 PostgreSQL/ES，向量通道依赖外部
> embedding 服务**。产品型号这类查询（`H105P`、`图冠系列`）本就不该指望语义相似度，
> 所以权重上精确 > 关键字 > 向量，且向量不可用时前两条仍然完整可用。
>
> 消费方有两处：HTTP 的 `GET /dataset/:id/search`，以及 `cmd/rag-test` 的离线评测。
> 对话侧的 `search_knowledge` 仍是 bindings 版本、没有接检索，所以「带引用输出」
> 尚未生效，见 [待完善项](known-gaps.md)。

拆分的收益是故障域隔离：embedding 或 Milvus 慢/挂只影响消费进度，不影响 HTTP 收发；
HTTP 重启也不会丢在跑的任务。代价是多了一个必须独立部署、独立配队列的进程，运维上
两个进程要能看到同一份 `asynq` 配置和同一个 Redis。

重入靠两道幂等：`documents.metadata.content_hash`（正文 SHA-256）让「内容没变的重建
索引」跳过重新切块；`embedPending` 每轮只挑 `vector_status=pending` 的 chunk，所以
重试是**按 chunk 收敛**的，已经写进 Milvus 的块不会重做。因此在跑的任务被取消
（关闭超时、进程退出）时**不能**写终态 —— 它会被退回队列重跑，写终态就会把一次正常的
重试变成永久失败。

任务带一个模式（`tasks.IndexMode`），决定要不要相信那个内容指纹：

| 模式 | 谁投的 | 行为 |
| --- | --- | --- |
| `CatchUp`（零值 / payload 里省略） | 创建文档、改正文 | 指纹一致 → 复用已有分块行；不一致 → 重切 |
| `Rebuild`（`"mode":"rebuild"`） | `POST .../reindex`（单篇 / 整库） | **忽略指纹**，一律重新解析 + 切块 |

分开的理由是「重建」与「补齐」要解决的问题不同：改正文之后重切是自然的（指纹变了），
但**解析器或切块配置变了的时候正文没变** —— 只看指纹会把 reindex 变成一次什么都不做的
空转，而这恰好是它最主要的用途（修回元数据形态、修完失败的文档）。模式放在 payload
而不是进程内存里，是为了让意图跨进程、也跨重试活下来。

`Rebuild` 会让分块行拿到新的自增 ID，所以 `rechunk` 里必须显式调用
`DeleteByDocument` 清掉旧的 ES 文档：旧 `_id` 再也不会被覆盖，不删就是永久残留。

文档和切块记录保存在 PostgreSQL；Milvus 仅保存 `chunk_id` 与向量。最终查询会再次按文档可见性过滤，因此向量库的候选结果不能直接暴露给用户。

文档正文保存在 `documents.content`，这是正文**唯一的真相**：写进来的是它，索引时
读的是它，召回补全文读的也是它。产品型录里一块的 YAML 头不进这一列 —— 它在写入
时就被解析成业务键铺进 `documents.metadata`（型号、系列、品类、规格明细），正文
只留 Markdown 部分。

`documents.source` 仍然存在，但它是**逻辑标识**而不是磁盘路径：检索结果靠它做引用
与去重、评测用例集靠它写期望命中的文档、ES 里有它的独立字段。形状沿用
`documents/<数据集 id>/<主干>.md`，由服务端按标题或型号派生（`rag.DocumentSource`），
写入之后不再随内容变动 —— 换一次正文不该换一个引用名。

不再有「注册外部文件」这条路径：正文只能从 `content` 字段进来。过去那种「托管目录
内的可改、root 下的只读」的区分随之消失 —— 它本来就是文件存储的产物。

> **迁移**：正文搬家之前建的文档，`content` 为空而正文还在 `knowledge.root` 下。
> worker 索引到这种文档时会去旧目录读一次、把正文（去掉 YAML 头）补进库
> （`Indexer.importLegacyContent`），导入过一轮就不再读文件。这是**一次性**通路：
> `rag.LegacyContentReader` 只读、不建目录、不写任何东西，等存量文档都导入完之后
> 它和 `knowledge.root` 那段配置可以整块删掉。旧文件里若有多个产品块，导入会明确
> 失败并提示重新上传（那份文件对应多条文档，硬塞给一条会把内容算错）。

| 模块 | 单一职责 | 不负责 |
| --- | --- | --- |
| `rag.LegacyContentReader` | 在 `knowledge.root` 内安全**读取**旧正文文件（含软链与路径穿越校验），只服务存量数据的一次性导入 | 写入、文档归属、切块、索引 |
| `rag.FileLoader` | 在受控目录内安全读取支持的文本文件（`cmd/ragserver` 的演示链用） | 知识库归属、权限和入库 |
| `rag.Pipeline` | 把「解析 → 切块」串成一条 eino Chain。两条入口：`IngestContent`（正文已在内存，知识库索引走这条，不需要任何目录）与 `IngestFile`（从 `DocRoot` 读文件，产品拆分的接缝是 `FileLoader` 里那张 parser 注册表）；`Config.Store` 为 nil 时链尾就停在切块 | 文档归属、分块状态机、检索授权 |
| `application/knowledge.Service` | 校验入参、把正文写进 `documents.content`、写 `documents` 行、提交后投递索引任务；文档增删改查编排与实时分块统计 | 切块、embedding、写向量 |
| `knowledge.Indexer` | 消费索引任务：取 `documents.content`（空则从旧文件导入一次）→ 驱动 `rag.Pipeline` 切块 → 落 pending 行 → embedding → 写向量 → 写检索索引（ES，未配置则跳过），并维护 chunk/document 的索引状态 | HTTP 入参校验、检索排序 |
| `platform/queue.Producer` | 把「这篇文档要索引」投进 Redis 队列（HTTP 侧持有的抽象） | 决定索引策略、写向量、消费 |
| `platform/queue/asynq.IndexQueue` | 把 `Producer` 适配成应用层的 `knowledge.IndexTaskQueue` 端口 | 业务决策 |
| `platform/queue/asynq.AsynqClient` | 同时是 asynq 的 Producer 与 Consumer（后者实现 `service.Service`，由 `cmd/worker` 单独启停） | 业务处理与存储访问 |
| `rag.Store` | 检索的三个通道：精确通道（结构化字段逐字相等，走 ES `term`；未配 ES 返回空并标记降级）、关键字通道（配了 ES 走 BM25，没配回落 PG 词元子串匹配）、向量通道（Milvus 取 chunk_id → 回 PostgreSQL 补齐正文与 provenance）；词法/精确通道不再要求 `vector_status=indexed`，向量通道仍只召回已索引分块 | 融合排序、授权决策 |
| `rag.HybridRetriever` | 把三个通道的结果做**加权** RRF 融合（`Σ weight/(k+rank)`，权重见配置 `retrieval`），按名次而非原始分数量纲定序，套用 `Filter`（数据集范围 + 可见性 + 启用状态）；返回 `Report` 记录各通道成败与降级。**输出恒为分块**，归并按库类型由用例层做 | 决定结果粒度、直接对外格式化回答 |
| `rag/grouping` | 按 `dataset.type` 解析召回结果的归并粒度（`chunk` / `document`），并把「要 N 条结果」折算成「向检索侧要多少分块」（`FetchPlan`）—— 见「召回结果的归并粒度」与「条数的单位」 | 判定命中的口径、RRF 融合 |
| `platform/storage/es` | 关键词索引的读写：索引生命周期（建/校验/演进）、按 `_id=chunk_id` 的幂等批量写入、按文档清理、多字段 BM25 查询（`bool.should` 组合 `multi_match` 与各字段 `.keyword` 子字段的 `term`）；精确查走 `SearchExact` | 权限判定、正文存储、向量检索 |
| `eval` | 离线召回评测：装载金标用例、驱动检索、按配置粒度整理结果明细、折算成 Recall@K/MRR/ACL 泄漏/P95 并按门禁判定。质量指标恒按文档去重，不随粒度变 | 在线检索、生成式回答质量 |
| `tool.KnowledgeSearch` | 将经过认证的主体和服务端 KB 白名单转换为检索请求并格式化引用（**当前是 bindings 版本，尚未接检索**） | 让模型决定可访问的知识库 |

`application/knowledge` 是这条链路上的**唯一业务入口**：HTTP transport 只认识
`Service`，不认识队列；`Indexer` 只认识任务 payload，不认识 HTTP。两边各自依赖
`DocumentContent` / `VectorIndex` / parser 注册表这些接口，具体实现在组合根里接线。

### 检索通道与元数据检索面

召回分三个通道，各自独立成败，融合靠加权 RRF：

| 通道 | 实现 | 排序依据 | 依赖 |
| --- | --- | --- | --- |
| 精确 | ES `term` 打在挂 `keyword` 子字段的业务字段上 —— 当前是 `model`、`product_id`、`product_name`、`series_name`、`family_prefix`、`category_l1`、`category_l2`、`variants` | 逐字相等 | ES；**未配 ES 时该通道返回空并标记降级** |
| 关键字 | 配了 ES 走 `platform/storage/es` 的多字段 BM25（`bool.should`：`multi_match` + 精确 `term`）；没配回落 `rag.Store.searchBySubstring` 的 PostgreSQL 词元子串匹配 | 词频 + IDF + 字段长度归一化 / 命中词元比例 | PostgreSQL，可选 ES |
| 向量 | Milvus 取 `chunk_id` → 回 PostgreSQL 补齐正文与 provenance | 向量距离 | Milvus + embedding 服务 |

精确通道的入选条件是「业务字段 + `keyword: true` + `boost > 0`」**三条同时满足**，
共有字段（`source`/`title`/`heading_path` 虽然也挂了 `keyword`）不参与 —— 它们走
`multi_match` 打分就够了，再进精确通道只会让文件名撞上查询串时压过型号命中。

精确通道内部只排「是否逐字相等」，**不额外放大权重**：「精确命中比词元命中更值钱」
这件事由下面那条独立的通道权重表达。两处都放大等于把同一个判断算两遍，调参时没人
说得清最后是几倍。

三通道的权重与候选上限来自配置的 `retrieval` 段（`rag.PolicyFromConfig`）：

```yaml
retrieval:
  exactWeight: 3.0        # 精确优先
  keywordWeight: 1.5
  vectorWeight: 1.0
  rrfSmoothing: 60        # RRF 平滑 k
  exactCandidateLimit: 20
  keywordCandidateLimit: 30
  vectorCandidateLimit: 30
```

权重为 0 即关闭该通道（不出现在 `Report` 里，也不计为降级）。融合公式是
`score = Σ weight / (k + rank)`，加权 RRF 的好处是**不需要跨通道归一化原始分数** ——
BM25 分、`term` 分与向量距离量纲完全不同，按名次求和天然可比。

**为什么关键字优先于向量。** 产品资料检索的典型查询是型号、系列、品类这类**精确串**
（`H105P`、`图冠系列`），它们要么在元数据里逐字存在、要么不存在，语义近邻对短串既不
稳定也不可解释；而这些串恰恰是用户最在意的召回目标。所以权重上精确 > 关键字 > 向量，
向量更多负责「用自然语言描述需求」那一类查询的兜底。

`exactTerms` 把查询按非字母数字边界切成至多 8 个词（不做中文 2-gram），每个词单独对
`.keyword` 子字段发 `term` —— 这样一个「H105P 图冠」的查询可以分别命中型号与系列，
而不是要求整串相等。

**ES 不参与正确性判定。** 它只给名次：命中的分块一律回 PostgreSQL 取正文、`source`、
`visibility`、`owner`，ACL 过滤以 PostgreSQL 现值为准。索引里那份 `visibility` 是写入
那一刻的快照，文档转私有或改归属之后就过期了，拿快照判权限等于越权。

### 召回结果的归并粒度

上面三条通道返回的都是**分块**，但「一条结果」是什么由知识库类型决定 —— 配置在
`knowledge.recallGrouping`（键是 `dataset.type`，`default` 兜底，见
`rag/grouping.Policy`）：

| 粒度 | 一条结果 = | 适合 |
| --- | --- | --- |
| `document`（缺省） | 一篇文档：取命中的最高分块作代表决定名次，`content` 换成**整篇文档**的正文 | 产品型录：一篇文档就是一个产品，型号/系列/规格与正文都在那一篇里 |
| `chunk` | 一个分块 | 普通文档库：一篇长文切成几百块，归并成一条等于什么都没返回 |

粒度是**按库**选的，不是按系统选的：一条检索链路要同时服务两类库，写死在其中一边，
另一边必然错。而错得不像 bug —— 产品库里表现为「同一个产品在结果里出现八次」，
文档库里表现为「明明有内容却只回一条」，两种都会被当成召回质量问题去查。

落点在两处，各自都有理由：

| 位置 | 做什么 | 为什么在这一层 |
| --- | --- | --- |
| `knowledge.Service.Search` | 按数据集类型解析粒度、折算取数预算、归并、补全整篇正文 | 它是唯一知道数据集类型的地方；且写在这里，换一个 `Searcher` 实现也不会漏掉 |
| `cmd/rag-test` / `internal/eval` | 按 `-dataset-type` 解析粒度，同一套折算规则取数，结果明细按它列出 | 评测报的条数必须和接口返回的是同一个单位，否则评测与线上各说各话 |

两条容易踩的边界：

1. **质量指标不跟粒度走。** 评测的 `Recall@K` / `MRR` 恒按文档去重，只有结果条数
   跟着粒度。让质量指标随粒度变，等于「把 chunkSize 调小」就能把分数刷上去。
2. **归并不重排。** 两种粒度下都保持检索给出的相关性顺序，不按分数重排 —— 重排是
   检索侧（reranker）的职责，在这里顺手排一遍会让「权重改动有没有生效」不可观测。

### 条数的单位：取数与归并的折算

`top_k` 的单位是**结果条数**，而检索侧的单位是**分块** —— 两者只在 `chunk` 粒度下
碰巧相等。`document` 粒度下直接拿条数当分块数要，得到的结果必然缩水，缩水的倍数
还由切块密度决定：

```
top_k=20 → 检索侧给 20 个分块 → 每篇切 5 块 → 归并出 4 篇
```

这个数字会随着调小 `chunkSize` 自动变大，看起来像召回质量在波动，实际是单位没对齐。
所以取数是**按预算来的**（`rag/grouping.FetchPlan`）：

| 规则 | 值 | 理由 |
| --- | --- | --- |
| 起始预算 | `want × 4`（`document` 粒度） | 一篇文档常见切成 2~4 块，一轮就能取满；预算不能一次拍太大，兑换率事先不知道 |
| 加码 | 不够则翻倍 | 退化切块（一篇几十块）下小倍数没救，一次拍死要么不够要么白拉几倍候选 |
| 收手 | 取满 / 检索侧见底 / 预算到顶（512） | 「给不满预算」就是「库里就这么多」，必须立刻停 —— 否则会一路翻倍去问一个已知没有更多内容的池子 |
| `chunk` 粒度 | 预算 = 条数，只跑一轮 | 那一条结果就是一个分块，放大换不来任何东西 |

同一次召回的 **`matched_chunks`（归并前命中几块）与 `chunk_budget`（实际预算）**
会随结果一起返回，它们是「为什么只有几条」的现场证据：命中 40 块归并出 4 篇，说明
库里匹配的就这 4 篇；给满 160 块却只归并出 4 篇，说明切块过碎把候选名额吃光了。
没有这两个数，两种情况的响应一模一样，而下一步动作完全不同。

`document` 粒度还会把代表块的 `content` 换成**整篇文档的正文**（按命中的
`document_id` 一次查回 `documents.content`，不是把命中块拼起来 —— 切块是带 overlap
的滑动窗口，拼回去会重复，还会丢掉没命中的段落）。代表块回答的是「这篇文档为什么
被召回」，它不是阅读的单位：产品型录里只回一块，等于把一个产品拆开只给一半。整篇
正文超过 `knowledge.maxResultBytes` 时按 rune 边界截断，并在 `content_truncated` 上
显式标记 —— 悄悄截断会让下游拿半份规格当全份用。

**检索面是一份显式声明，不是一个写死的 mapping。** 字段分两层：

| 层 | 定义在 | 内容 |
| --- | --- | --- |
| 共有字段 | 代码（`es/baseFields`） | `chunk_id` / `document_id` / `dataset_id` / `chunk_index` / `source` / `title` / `heading_path` / `content` / `metadata_text` / `visibility` / `owner` / `indexed_at` / `mapping_rev` |
| 业务字段 | `es.mappingFile` 指向的文件（默认 `configs/es/chunk_mapping.yaml`） | 字段名、类型、从元数据的哪个路径取值、权重、是否挂精确匹配子字段 |

分两层的理由是它们的**变更节奏不同**：共有字段由写入端（`ChunkDoc`）直接填，每个索引
都一样；业务字段按产品线变化，换一套型录只改配置文件、不动代码、不发版。

这份默认映射的字段与权重（`configs/es/chunk_mapping.yaml`）：

| 字段 | 来源 | 权重 | 为什么 |
| --- | --- | --- | --- |
| `model`、`product_id` | 分块元数据 | 5 | 型号与产品 ID 是业务主键式信息，搜它几乎必然就是要那一篇 |
| `product_name` | 分块元数据 | 4 | 产品名 |
| `series_name` | 分块元数据 | 3 | 系列名（图冠系列） |
| `title`、`source` | `documents` 表（共有字段） | 3 | 文档标题、文件名 |
| `family_prefix`、`category_l1`、`category_l2`、`variants` | 分块元数据 | 2 | 系列前缀与品类：命中它们只说明「属于这一大类」 |
| `metadata_text` | 没单独成列的业务元数据（共有字段） | 2 | 规格明细与上传时带的任意键 |
| `heading_path` | 分块（共有字段） | 2 | 标题路径 |
| `content` | 分块正文（共有字段） | 1 | 正文 |

元数据排在正文之前是本系统一个明确的取舍：型号、系列、品类**通常只出现在产品块的
YAML 头里**，正文里一次都不出现，所以「按型号找资料」能不能命中，几乎完全由这张表
决定。反过来，只在正文里顺带提到某个型号的文档拿 `content^1`，排在元数据命中的文档
之后。

**为什么 mapping 与权重必须出自同一份声明。** 分开写不会报错：`multi_match` 里不存在
的字段静默贡献 0 分，唯一的表现是「召回变差了」—— 排查会一路怀疑到分词器、embedding
和切块策略上。所以 `Mapping` 同时派生三样东西：建索引的 `properties`、检索的字段权重
表、以及写入时「从元数据的哪个路径取值」。改一处，三处一起变。

**新增元数据键不必改 mapping。** 没有单独成列的值（`specs_from_doc` 里嵌套的规格明细、
`source_doc`、上传时 `metadata` 带着的任意键）摊平进 `metadata_text`，从写入那一刻起
就可检索，代价是它们共享同一个权重。要给它独立权重时，在映射文件里加一条即可。

**已经单独成列的值不会再进 `metadata_text`。** 取值与摊平是同一次判断的两面
（`es.Mapping.Project`）：声明过的路径是**搬家**，不是「再加一份」。两边各留一份不增加
召回（同一个词在这篇文档里本来就命中），只会抬高它的 tf —— 而中文业务词（铰链 / 固装 /
系列名）几乎每篇文档都有、IDF 接近零，本来就不该左右排序，tf 的差异反而会让它们主导
排序，表现是「问固装铰链，结果全被拽到只含铰链的文档上」。排除只针对确实取到值的路径：
路径拼错、该文档没有这个键、数组没声明 `multi` 时取不到值，那些内容继续留在兜底字段里，
不会被静默抹掉。

**嵌套值要单独成列，必须下钻到具体键。** `from` 只认「map 逐层下降」，不认数组下标，
也不接受容器本身：

```yaml
- name: door_material              # 字段名不能带点，另起一个平铺名
  from: specs_from_doc.door_material
  type: text                       # 文本规格可打分
  boost: 3
- name: open_angle_deg
  from: specs_from_doc.open_angle_deg
  type: integer                    # 数值规格只过滤/排序 —— 且不能给 boost
```

两条容易踩的边界：

- **不要声明 `from: specs_from_doc`（整块）。** 嵌套对象转不成字段值，`Extract` 会
  **静默跳过**，于是字段建出来了却永远是空的，而 `MissingPaths` 也放行它（路径确实
  存在，只是值不是标量）。症状仍是「按规格搜不到」。这类「路径存在但形状不对」的错误
  由 `TestShippedFieldsResolveToScalars` 拦住，它比 `MissingPaths` 多一层形状核对。
- **数值规格声明成统一类型。** parser 里 `open_angle_deg` / `cup_diameter_mm` /
  `door_thickness_min_mm` / `door_thickness_max_mm` 是 `*int`；声明成 `text` 会经
  `toString` 变成 `"100"` 参与检索，声明成 `integer` 就只能过滤/排序。**两者之间切换
  不能就地改** —— 字段类型变了，必须换 `es.index` 名重建。

**机制键在进入索引之前就被剔掉**（`application/knowledge/keyword_doc.go` 的
`searchableMetadata`）：`doc_id`、`content_hash`、`visibility`、`owner`、`source`、
`title`、`heading_path`、`chunk_index`、`dataset_id`、`type`，以及**所有下划线开头的键**
（loader 注入的 `_source` / `_extension` / `_file_name`）。它们要么已有独立字段，
要么是枚举值 —— 把 `visibility=system` 索引进去，搜「system」就会命中全库。按下划线
整类拦而不是逐条登记，是因为 loader 哪天再加一个 `_xxx` 时逐条登记一定会漏。

**落库前还会清掉 PostgreSQL 存不下的字符。** `0x00`（NUL）在 `text` 与 `jsonb` 里都
非法，插进去会让整批 insert 一起失败（`invalid byte sequence for encoding "UTF8": 0x00`），
一份带 NUL 的 PDF 转换产物会让这篇文档永远索引不上。清洗（`sanitizeText` /
`sanitizeMetadata`）挂在 `buildChunks` 上，递归覆盖 `specs_from_doc` 这类嵌套值与
`variants` 这类数组。

**映射与 parser 的一致性由测试守住。** 声明里的每个 `from` 路径都必须在
`internal/rag/parser` 产出的元数据里真实存在，否则那个字段永远是空的（表现同样是
「按型号搜不到」）。`mapping_contract_test.go` 用**真实的 `ProductParser`** 输出核对两件
事：路径都能取到值；parser 产出的每个顶层键都有归宿（单独成列，或显式列进「进兜底
字段」的清单）。parser 那边新增一个元数据键而没人决定它怎么检索时，这条测试会失败。

**索引形态的演进。** `cmd/worker` 启动时调 `EnsureIndex`：索引不存在就按当前映射建
（并关掉动态映射，见下）；已存在就先校验分词器（不一致只能换索引名重建），再把映射
新增的字段补写上去 —— ES 允许往已有 mapping 里加字段，所以加检索面不必重建索引。
但**补写 mapping 不等于已有文档有了值**，它们要重新索引才会带上这些字段。

这类文档靠**形态指纹**识别：写入端给每份文档盖一个 `mapping_rev`（`Mapping` 声明本身
的哈希），`cmd/rag-test` 的预检查出「指纹与当前映射不一致」的文档就提示对它们触发
reindex。指纹只覆盖**索引期**形态（分词器、字段名/类型/取值路径），权重被排除在外 ——
权重是查询期的东西，改它不需要重建索引，算进去会逼出一次假的全量重建。用指纹而不是
手写版本号，是因为映射搬到配置文件之后，人工维护的版本号就成了第二处需要同步的地方。

动态映射取 `false`：映射文件里漏声明的字段若被 ES 自动加成 `text`，用的是索引默认
分词器（中文切成单字），既不报错也不影响已有字段，只会悄悄多出一批检索效果很差的
数据。`false` 让漏声明表现为「字段没进索引」（值仍然完整保存在 `_source` 里）。
`EnsureIndex` 会在启动时把当前字段类型补写回索引，类型冲突会当场失败并提示换索引名；
但**漏声明不会在启动时报错** —— 它只能靠映射与 parser 的一致性测试（上面的
`mapping_contract_test.go`）提前拦住。

`es.analyzer` 是所有 text 字段的默认分词器（单个字段可在映射里覆盖）。事后改它必须
换 `es.index` 版本号重建：改配置却忘了换索引名，进程照常启动、写入照常成功，只有检索
命中率悄悄变差。`EnsureIndex` 会在启动时直接报出来。

### 进程编排

`cmd/restapi` 与 `cmd/worker` 各自只挂一个 `service.Service` 到 `service.ServiceGroup`：

| 进程 | 挂载的 service | 关闭预算来自 |
| --- | --- | --- |
| `cmd/restapi` | `transport/restapi.Server`（HTTP 监听） | `internal/transport/restapi/etc/restapi.yaml` 的 `Shutdown.WaitTime` |
| `cmd/worker` | `asynq.AsynqClient`（消费端，`Start()` 阻塞到 `Stop()`） | `configs/config.yaml` 的 `asynq.shutdownTimeoutSeconds` |

HTTP 进程有一个容易踩的细节：监听的关闭**不由** `Stop()` 触发。go-zero 把监听的关闭
挂在 `core/proc` 的全局 shutdown 链上（SIGTERM/SIGINT 触发），而那条链只能被 notify
一次 —— listener 内部会 `WaitGroup.Done`，再来一次就 panic —— 所以 `Stop()` 只负责等
监听退出，关闭权交给 proc。`Shutdown.WaitTime` 必须大于本次请求链路上最慢的一次
同步操作，否则还没有响应完的请求会被 go-zero 直接 `Kill` 掉（默认预算只有 5.5s）。

worker 进程的关闭预算含义不同：它等的是**正在跑的索引任务**排空。任务可能正卡在
embedding 的网络往返上，`asynq.shutdownTimeoutSeconds` 要按任务实际耗时配；配小了
超时的任务会被退回队列重跑，白烧一次 embedding（只是浪费，不会错 —— 靠
`content_hash` 与 pending 语义收敛）。

两个进程都拒绝「半个配置」启动：HTTP 在 `asynq.enabled=false` 时仍可启动（只是文档
写操作会同步失败），worker 则直接拒绝启动，因为一个没有队列可消费的 worker 只会空转。

知识库、文档和授权关系由 `internal/transport/restapi` 的 admin 接口维护
（契约见 `internal/transport/restapi/docs/dataset/dataset.api` 与
`admin/admin.api`）；领域逻辑在 `internal/application/knowledge`，切块、正文
文件与向量基础设施在 `internal/rag`。`agent_datasets` 以 API Key 的 `subject`
为主体保存可访问 dataset ID；`search_knowledge` 在每次调用时读取这些绑定，
因此未绑定主体绝不会检索任何资料，模型也不能通过工具参数扩大范围。
`Metadata` 在入队时统一合并文档字段、分块序号和标题路径，避免各调用方直接
修改持久化元数据。`SearchScope` 的知识库 ID 是强制白名单：空列表表示无检索
权限，绝不会回退为全库搜索。

当前系统只有固定内部执行器 `knowledge_agent`；它代表发起请求的 API Key 主体运行。未来如加入可持久化的 Agent 实体，可将绑定主体从 `subject` 扩展为 Agent ID。

### 链路追踪

一篇文档的索引要跨两个进程，所以「链路」在这里是**真的跨进程**，不是同一进程内的
span 嵌套。链路形状：

```text
POST /api/v1/dataset/:id/documents/upload     （go-zero TraceHandler，Server span）
  knowledge.create_document                   （应用层用例）
    queue.publish knowledge:index             （Producer span，producer 的父）
      ──── Redis ────  traceparent 随 payload 一起过去  ────
        queue.process knowledge:index         （Consumer span，上一条的子）
          knowledge.index_document
            knowledge.ingest_document         （Pipeline：读入 → 拆产品 → 切块）
            knowledge.embed_batch             （每批一次，含 embedding + Milvus）
```

**跨进程怎么接上的。** 队列这层唯一能带过去的东西就是 payload。asynq 适配器在投递
时把业务 payload 包一层信封 `{"v":1,"trace":{...},"body":<原 payload>}`，`trace` 里是
W3C `traceparent`；消费端拆信封、恢复上下文，再开一个消费 span。所以：

- 业务代码**不需要**知道链路的存在 —— 注入与提取都发生在 `platform/queue/asynq`，
  payload 契约（`platform/queue/tasks`）没有多出任何链路字段，将来新增任务类型自动
  获得透传。
- 解码必须容错：worker 与 HTTP 是两个进程，滚动升级期间队列里同时存在带信封和不带
  信封的消息。版本号 `v` 参与判定，旧格式（`v` 对不上或没有 `body`）会被原样当作
  payload 处理；连 JSON 都不是的 payload 也一样。**能容忍旧 payload 是硬要求**：
  认不出来会让积压的任务全部变成「解码失败」并反复重试。

**两个进程装 TracerProvider 的方式不同，这是最容易出错的地方。**

| 进程 | provider 谁装 | 服务名来自 |
| --- | --- | --- |
| `cmd/restapi` | go-zero 的 `ServiceConf.SetUp()` → `trace.StartAgent` | `internal/transport/restapi/etc/restapi.yaml` 的 `Telemetry.Name` |
| `cmd/worker` | `observability.SetupTracing`（组合根显式调用） | `configs/config.yaml` 的 `observability.workerServiceName` |

- **worker 必须自己装**：它不跑 rest/rpc，没有任何东西会替它调 `SetUp()`。没有
  provider 时 `otel.Tracer()` 是空实现，从 payload 里恢复出来的上下文无处落地 ——
  表现为「HTTP 有 trace，worker 日志里连空的 trace 字段都没有」，而且完全不报错。
- **restapi 不能再装**：`otel.SetTracerProvider` 是全局的、后装覆盖先装。两个都装会让
  其中一方的服务名/环境静默失效。
- 两个服务名**必须不同**：链路是「HTTP 投递 → worker 消费」的跨服务调用，同名会被
  渲染成「服务自己调自己」，异步那一段就藏起来了。
- 退出时冲刷要用**独立的超时上下文**：worker 的 ctx 是信号上下文，进程退出时已经
  被取消，拿它调 `Shutdown` 会把 Batcher 里还没发出去的 span 直接丢掉。

**开关语义**：`otlpEndpoint` 留空也照样安装 provider（只是不导出），`traceSampleRatio`
<= 0 当作 1.0。两条都是为了避免「少配一个字段 → 链路静默消失」——这种坏法比直接报错
难查得多。`otlpEndpoint` 填上之后 span 才开始真正导出。

本地的上传、分块、索引和检索验证步骤见 [RAG 本地验证](rag-testing.md)。

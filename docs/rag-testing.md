# RAG 本地验证

本文验证完整的文档索引链路：创建数据集、写入一篇 Markdown、请求内落库并投递任务、
由独立的 worker 进程切块并异步生成向量写进 Milvus。全程走 REST 接口，不需要往目录里
手工塞文件。

## 前置条件

1. PostgreSQL、Milvus、Redis 都可访问，且 `configs/config.yaml` 里的 `storage`、
   `milvus`、`embedding`、`asynq` 配置与实际环境一致。
2. Elasticsearch 可选：`es.address` 填了就启用 BM25 词法通道，留空则词法通道回落
   PostgreSQL 子串匹配（**此时按产品型号搜不到东西** —— 型号只存在于分块的
   `metadata` 列里，见「关键词通道与元数据」一节）。启用时需要：

   ```yaml
   es:
     address: ["https://es-xxxx.tencentelasticsearch.com:9200"]
     index: "eino_document_chunks_v1"
     analyzer: "ik_max_word"        # 集群需装 IK 插件；没装用内置 cjk
     username: "elastic"
     passwordEnv: "EINO_ES_PASSWORD"
     caCertPath: "/etc/es/ca.crt"   # 托管集群的 CA 证书
     mappingFile: "configs/es/chunk_mapping.yaml"   # 检索面声明；必填
   ```

   索引由 `cmd/worker` 启动时创建/校验，不需要手工建。`analyzer` 写错（集群没装
   IK）会在 worker 启动时以 400 失败，报错里会提示改用 `cjk`。

   **`mappingFile` 是必填项**：它决定索引里有哪些业务字段（型号、系列、品类）、
   各自权重、以及值从分块 `metadata` 的哪个路径取。ES 启用却留空会在启动时直接报错。
   换一条产品线时改这个文件即可，不需要动代码。
3. 设置运行所需的环境变量（两个进程读同一份配置，所以都要设）：

   ```bash
   export EINO_STORAGE_PASSWORD=...
   export EINO_EMBEDDING_API_KEY=...   # worker 需要；没设 worker 会拒绝启动
   export EINO_API_KEY_ADMIN=...
   # Redis 需要口令时，把 asynq.redis.passwordEnv 设成下面这个名字
   export EINO_REDIS_PASSWORD=...
   # 配了 es.passwordEnv（默认 EINO_ES_PASSWORD）时需要
   export EINO_ES_PASSWORD=...
   ```

4. 在仓库根目录执行命令。正文落在 `knowledge.root`（`configs/config.yaml` 里当前是
   `./tests/knowledge`）下的 `documents/` 托管子目录里。

## 启动服务

需要**两个**进程，分别开一个终端：

```bash
# 终端 1：消费端，负责切块 + embedding + 写 Milvus
go run ./cmd/worker

# 终端 2：HTTP，负责接收请求 + 投递任务
go run ./cmd/restapi
```

- `cmd/worker` 启动时会先建/校验 Milvus collection（维度、metric、load 一次往返），
  然后才开 Consume。启动日志里出现 `eino worker started` 且 `queues` 字段含
  `index` 才说明它真的在领任务。
- `cmd/restapi` 监听 `internal/transport/restapi/etc/restapi.yaml` 里的 `Host`/`Port`
  （默认 8090）。
- 两个进程都会在启动时探一次 Redis，地址或密码不对会直接失败退出。
- 只起了 HTTP 没起 worker 时，写文档的请求**仍然成功**（正文落盘、任务进队列），
  文档会一直停在 `indexing` —— 这是最容易误判成 bug 的一种状态。

## 写入一篇文档

```bash
ADMIN=$EINO_API_KEY_ADMIN
BASE=http://127.0.0.1:8090/api/v1

# 建数据集，记下返回体里的 id
curl -sS -X POST "$BASE/dataset" \
  -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' \
  -d '{"name":"demo","description":"本地验证","visibility":"system","type":"document"}'

# 写入正文：content 会被写进 knowledge.root/documents/<datasetId>/ 下的托管文件
curl -sS -X POST "$BASE/dataset/1/documents" \
  -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' \
  -d '{"title":"guide","content":"# 标题\n\n正文……"}'
```

写入请求内的链路**不包含切块**：

```text
POST /dataset/:id/documents          ← cmd/restapi
  -> ContentStore 把正文写到 knowledge.root/documents/<id>/<slug>.md
  -> 事务写入 documents，status=indexing
  -> 提交后投递 asynq 任务（队列 index，类型 knowledge:index）
  -> 返回

consume                              ← cmd/worker（另一个进程）
  -> rag.Pipeline 的 ingest 链（与 cmd/ragserver 共用同一份实现）
     -> FileLoader 按 documents.source 把文件读回来
     -> Parser 按 front-matter 拆出 N 个产品块（当前实现，见 known-gaps）
     -> MarkdownChunker 逐块切分块（标题优先，超长按字符数滑窗）
  -> 事务写入 document_chunks，vector_status=pending
  -> 分批 embedding -> Milvus upsert
  -> 写检索索引 ES（_id = chunk_id，按 chunk 覆盖；配了 es.address 才有这一步）
  -> chunk 标 indexed；全部分块追平后 documents.status = ready
```

请求**不等待 embedding，也不切块**：返回时 `chunk_count` 和 `indexed_chunk_count`
都还是 0 —— 因为「有几个分块」要等 worker 按内容拆出产品块才知道。这两个数是接口
每次从 `document_chunks` 实时聚合的，不是 `documents` 上的列，所以它们会随着 worker
推进而增长，不是「创建时就定好的值」。

## 等待索引完成

```bash
curl -sS "$BASE/dataset/1/documents/1" -H "Authorization: Bearer $ADMIN"
```

`status` 变成 `ready` 且 `indexed_chunk_count == chunk_count` 即完成。一直停在
`indexing` 说明 worker 没跑或 Redis 不可达；变成 `failed` 说明重试次数已用尽。

## 查库核对

```sql
SELECT
  d.source, d.title, d.status,
  c.chunk_index, c.heading_path, c.vector_status, c.indexed_at
FROM documents AS d
JOIN document_chunks AS c ON c.document_id = d.id
WHERE d.id = 1
ORDER BY c.chunk_index;
```

预期 `documents.status = 'ready'`，每行 `vector_status` 都是 `indexed`。

## 排错

- **写文档的请求直接报错**：队列不可用。投递失败会让写请求失败，不会静默落库
  —— 理由见 [待完善项](known-gaps.md) 里的「文档索引的异步边界」。
- **文档停在 `indexing` 且 `chunk_count` 恒为 0**：worker 没在跑，或者任务投进了
  没人消费的队列。
  1. 确认 `cmd/worker` 进程活着，日志里有 `eino worker started`；
  2. 比对 `asynq.queues`（HTTP 与 worker 读同一份 `configs/config.yaml`）——
     漏配会让任务投进 `index` 却永远没人领，asynq 默认只消费 `default`；
  3. 直接看队列积压：`redis-cli -n 0 llen 'asynq:{index}:pending'`。
- **文档变 `failed`**：任务重试耗尽（`asynq.maxRetries`）。日志里搜
  `index document failed`，`retries_exhausted=true` 的那条会带原始错误（常见是
  embedding 鉴权失败或 Milvus 不可达）；修好外部依赖后用
  `POST /dataset/1/documents/1/reindex` 重来。注意进程在优雅关闭时被取消的任务
  **不会**写终态，它会退回队列由下个实例重跑。
- **看队列里还剩什么**：`redis-cli -n 0 keys 'asynq:{index}:*'`。重试耗尽的任务
  在 `asynq:{index}:archived`，超过保留期会被 asynq 自己清掉。

### 核对链路追踪

一次上传应当产出**一条** trace，横跨两个进程。核对步骤：

1. 上传响应头里的 `X-Trace-ID`（go-zero 从 `Telemetry` 段装出来的 trace id）就是
   这条 trace 的 id；
2. 用同一个 trace id 在两边日志里搜。**字段名不一样，值一样**：worker 走项目的
   slog，字段是 `trace_id` / `span_id`；HTTP 进程的访问日志是 go-zero 的
   `LogHandler` 经 logx 桥过来的，同样这个值在它那里叫 `trace`（键名由 `Log.TraceKey`
   决定，默认就是 `trace`）;
3. 在 trace 界面上应当看到两个服务名：HTTP 进程的来自 `etc/restapi.yaml` 的
   `Telemetry.Name`，worker 的来自 `configs/config.yaml` 的
   `observability.workerServiceName`。span 形状符合 `docs/architecture.md` 里
   「链路追踪」那一节。

排查用的两条：

- **worker 日志里没有 `trace_id` 字段**：worker 启动时会打一行 `tracing enabled`，
  先确认它出现了。没有 provider 时 `otel.Tracer()` 是空实现，链路会**静默**断在
  消费端 —— 不报错，也不会有空的 trace 字段。
- **HTTP 与 worker 的 trace id 不一样**：说明 payload 里的 traceparent 没被恢复。
  直接看队列里的字节：`redis-cli -n 0 lrange 'asynq:{index}:pending' 0 0` 里的
  payload 应当是一个 `{"v":1,"trace":{...},"body":{...}}` 信封。如果看到的是裸
  payload（没有 `v`/`trace`），说明投递侧是旧版本进程。

## 单元测试

正文文件存储的边界（路径穿越、软链逃逸、只读已注册文件、大小上限）无需外部服务
即可验证：

```bash
go test ./internal/rag
```

跨进程链路透传同样是纯内存可测的（信封编解码、旧 payload 容错、消费端恢复出的
trace id 是否等于投递端），不需要 Redis 与 collector：

```bash
go test ./internal/platform/queue/...
```

## 检索

> **当前状态：检索链路已经实现，HTTP 已接通、对话侧还没接上。** 三个召回通道
> （精确 + 关键字 + 向量）与加权 RRF 融合都在 `rag.Store` / `rag.HybridRetriever` 里，
> `cmd/rag-test` 与 HTTP 的 `POST /api/v1/dataset/:id/search` 跑的都是它们（见下一节）。
> 而 `cmd/restapi` 注册的 `search_knowledge` 仍是 bindings 版本，只回答「当前主体被
> 授权了哪些知识库」，不会真的召回内容。所以下面这条命令现在验证的是「Agent 路由 +
> 授权白名单」，**不是**「召回质量」—— 后者请用下一节的 `cmd/rag-test`。

索引完成后，可以直接用 HTTP 端点验证召回（注意响应里的 `channels` 与 `degraded`
能告诉你每条通道到底跑没跑）：

```bash
curl -s -X POST "http://127.0.0.1:8090/api/v1/dataset/<datasetId>/search" \
  -H "Authorization: Bearer $EINO_API_KEY_DEVELOPER" \
  -H 'Content-Type: application/json' \
  -d '{"query":"H105P","top_k":5}' \
  | jq '{channels, degraded, top_k, hits: [.data[] | {source, heading_path, score}]}'
```

返回的是**分块**（`chunk_id` / `heading_path` / `content`）而不是整篇文档：召回要回答的
是「哪一段文字能回答这个问题」，同一篇文档可能命中多个段落。`degraded` 为空表示三条
通道都正常参与；非空（比如 `["vector"]`）表示少了一条通道的贡献 —— 条数偏少或某个型号
搜不到可能就是它造成的。

索引完成后，用 `agent` 角色的 Key 调对话接口：

```bash
curl -N http://127.0.0.1:8090/api/v1/chat \
  -H "Authorization: Bearer $EINO_API_KEY_DEVELOPER" \
  -H 'Content-Type: application/json' \
  -d '{"message":"概括 guide 的主要内容"}'
```

根 Agent 会把知识类问题路由给知识 Agent，后者调用 `search_knowledge`。要验证索引
本身是否成功，看上面的「等待索引完成」和「查库核对」两节 —— 那两节不依赖检索链路。

## 召回评测

「索引成功了」和「问得出来」是两件事：上一节验证前者，这一节验证后者。做法是拿一份
固定的金标用例集（query + 期望命中的文档）去打真实检索链路，把命中折算成指标。

```bash
export EINO_STORAGE_PASSWORD=...
export EINO_EMBEDDING_API_KEY=...

go run ./cmd/rag-test                    # 跑随仓库分发的用例集
go run ./cmd/rag-test -v                 # 附带每条用例的明细
go run ./cmd/rag-test -topk 10           # 放宽 K，看漏召能不能捞回来
go run ./cmd/rag-test -out logs/report.json
```

### 跑之前会做几道预检

这些检查专门拦「跑了也必然全是 0」的情形 —— 静默的全 0 结果最容易被误读成
「召回质量差」，而真实原因往往是环境或用例集的问题：

1. **向量集合不可用** → 直接失败。集合是 `cmd/worker` 启动时创建的，先跑 worker。
2. **已索引分块为 0** → 直接失败。文档可能还在 `indexing`、索引任务失败、或没上传过。
3. **用例引用的 source 不在库中** → 全部不匹配时失败，部分不匹配时告警。
   写错 source 是评测集最常见的事故，而且症状和「召回不准」一模一样。
4. **配了 ES 但索引里一条文档都没有** → 直接失败。关键词通道必然全空、向量通道单打
   独斗，看起来就是「BM25 没用」。分块是在 worker 索引文档时写进 ES 的，确认 worker
   的 `es` 段配置与本次评测一致，并对已有文档逐个触发 reindex。
5. **ES 里有一批文档的映射形态与当前不一致** → 告警。它们是改过 `mappingFile`
   （增删字段或换 `analyzer`）之前写入的，`mapping_rev` 与当前指纹不同，新字段
   没有值，按型号、系列、品类可能都搜不到它们。触发 reindex 即可回填，见下一节。

预检里的 `document_status` 计数会一起打出来，用来发现「有文档卡在 `indexing`」。

### 三个召回通道与元数据

报告会写清本次真正跑起来的是哪几条通道，以及有没有通道降级 —— **同一份用例集在不同
通道组合下的分数不可直接比较**：

| 通道 | 启用条件 | 排序依据 |
| --- | --- | --- |
| 精确（`exact`） | 配了 `es.address` | 逐字相等（`term` 打在 `keyword` 子字段上） |
| 关键字（`keyword`） | 总是启用 | 配了 ES 走 `Elasticsearch BM25`（词频 + IDF + 字段长度归一化）；没配回落 `PostgreSQL 子串匹配`（「命中了几成词元」，长文档容易靠「什么都沾一点」排前面） |
| 向量（`vector`） | 配了 Milvus + embedding | 向量距离 |

三个通道按 `retrieval` 段里的权重加权融合（默认精确 3.0 > 关键字 1.5 > 向量 1.0），
**产品型号这类查询优先命中精确/关键字通道**，向量只作兜底。任一通道失败只降级不整体
失败：`vector` 依赖外部 embedding 服务，最容易先挂，挂掉后前两条仍然完整可用。

**元数据是关键词检索的主要命中面。** 产品的型号、系列、品类通常只出现在产品块的
YAML 头里（`product_id` / `model` / `series_name` / `category_l1` 等），正文里一次都
不出现 —— 所以按型号搜能不能命中，取决于这些键有没有进 ES。**哪些键进来、叫什么名字、
各自多重要、值从哪个路径取，全部由一份 mapping 文件声明**：`configs/es/chunk_mapping.yaml`
（`es.mappingFile` 指向它）。落地方式：

- mapping 里显式声明的业务字段各自单独成列，权重按声明（当前型号/产品 ID 是 5，
  产品名 4，系列/品类/变体 2~3），高于正文（详见 `docs/architecture.md` 的
  「检索通道与元数据检索面」）；
- 没有声明的元数据（`specs_from_doc` 里嵌套的规格明细、`source_doc`、上传时 `metadata`
  带的任意键）摊平进兜底字段 `metadata_text`，权重 2。**新增这类元数据键不需要改 mapping**；
  要把某个规格提成独立字段/独立权重，则在 mapping 里加一条 `from` 即可，也不需要改代码。
  注意这是**搬家**：声明过的路径不会再进 `metadata_text`。两边各留一份会把那个词的 tf
  抬到别人的两三倍，而「铰链」「固装」这类词全库都有、IDF 接近零，本来就不该左右排序
  —— 结果是「问固装铰链，全被拽到只含铰链的文档上」。想核对某篇文档到底索引了哪些值，
  直接看索引文档的 `_source.metadata_text` 即可。
- 字段上加 `keyword: true` 会同时挂一份不分词的子字段，**这一份才是精确通道的命中面**
  （`term` 打在 `model.keyword` 上）。当前声明了它的有 `model` / `product_id` /
  `product_name` / `series_name` / `family_prefix` / `category_l1` / `category_l2` /
  `variants`；没声明的字段不会进精确通道 —— 对不存在的子字段发 `term` 不报错、只是永远
  不命中，于是「精确匹配没生效」会伪装成「排序不够好」。共有字段（`source` / `title` /
  `heading_path`）虽然也挂了子字段，但**不进精确通道**，只走 `multi_match` 打分。

**元数据没进索引时，worker 会明说。** 一批分块全都没有业务元数据时，worker 会打一条
告警，带上 `dataset_type` 与 `source`：

```
knowledge: 这批分块没有可检索的业务元数据，检索索引里只会有基础字段  dataset_type=...
```

看到「ES 里只有基础字段」先 grep 这条日志，它能直接区分两种来源：数据集 `type` 没对上
解析器注册名（产品型录要填 `product`，否则退化成 TextParser，YAML 头没人解析），
以及正文里本来就没有产品块 YAML 头（普通 Markdown 走这条路是正常的）。

由此带出三个只在运维时才会遇到的坑：

- **补写 mapping 不等于已有文档有了值。** 往 ES 加检索字段是原地操作（worker 启动
  时 `EnsureIndex` 会补写），但已经写进去的文档不会因此长出这些字段。要给它们回填：

  ```bash
  curl -sS -X POST "$BASE/dataset/1/documents/1/reindex" -H "Authorization: Bearer $ADMIN"
  curl -sS -X POST "$BASE/dataset/1/reindex" -H "Authorization: Bearer $ADMIN"   # 整个数据集
  ```

  预检的第 5 条会把这类文档数出来。核对方式：重建前后跑一次 `cmd/rag-test`，按型号
  的那几条用例如果从「全不命中」变成命中，说明回填生效了。

  **reindex 是「重建」而不是「补齐」**：它会让 worker 忽略内容指纹、重新走一遍
  解析与切块。这一点很关键 —— 修好了解析器（比如把数据集的 `type` 改成 `product`）
  之后，正文本身没变，只看指纹的话这次修复会被判成「无需重切」，分块元数据永远停在
  旧形态。普通写入（创建文档、改正文）走的是「补齐」，内容没变时不会重切。
- **embedding 的单次请求条数上限会被服务方拒绝。** `embedding.batchSize` 是服务方的
  硬上限（DashScope 的 text-embedding-v3/v4 是 10），超过会回
  `400 batch size is invalid, it should not be larger than 10`。这个失败发生在向量与
  检索索引写入**之前**，所以症状是「整篇文档什么都没索引上」，而文档状态是 `failed`、
  worker 日志里只有一条 embedding 报错 —— 很容易被当成「检索质量差」。它与
  `indexer.batchSize`（一轮处理多少分块）是两件事，Embedder 会按前者自动切分请求。
- **换了 `es.analyzer` 必须换 `es.index` 名。** ES 的 mapping 不能就地换分词器，
  改配置却忘了换索引名，症状同样是「按型号搜不到」，而且没有任何报错。worker 启动时
  会校验并直接报错。**改已有字段的类型**（如 `text` → `integer`）同理，必须换索引名；
  只增字段或调 `boost` 不需要。

### 用例集格式

`internal/eval/datasets/*.jsonl`，一行一条，`//` 开头的行是注释，`-dataset` 可指定多个文件（拼成全量一起跑）。

| 字段 | 含义 |
| --- | --- |
| `id` | 用例标识，全局唯一（重复直接报错，否则指标会悄悄失真） |
| `query` | 检索查询 |
| `expected_sources` | 精确的 `document.source`，必须命中 |
| `expected_source_keywords` | 召回的 `source` 包含该片段即算命中 |
| `forbidden_sources` | 不该被召回；命中即越权，该用例直接判失败 |
| `min_results` | 结果条数下限；有答案的用例设 1。单位是**归并后的条数**，见下 |
| `scene` | 场景标签，报告按它聚合，用来定位是哪一类查询退化 |
| `top_k` | 覆盖默认 topK |
| `note` | 说明，失败时显示在明细里 |

**为什么有两种期望口径。** 托管上传的正文由 `ContentStore.Create` 用「slug + 纳秒
时间戳」命名，重新上传一次路径就变，把精确路径写进用例集等于让它活不过一次重传。
这类语料用 `expected_source_keywords`。自己注册进 `knowledge.root` 的文件路径可
预知，用 `expected_sources` 更严格。`forbidden_sources` 只支持精确匹配。

### 召回结果的归并粒度

「一条结果」是什么，由知识库类型决定 —— 配置在 `configs/config.yaml` 的
`knowledge.recallGrouping`（键是 `dataset.type`，`default` 兜底）：

| 粒度 | 一条结果 = | 适合 |
| --- | --- | --- |
| `document`（缺省） | 一篇文档（取命中的最高分块作代表） | 产品型录：一篇文档就是一个产品，型号/系列/规格与正文都在那一篇里 |
| `chunk` | 一个分块 | 普通文档库：一篇长文切成几百块，归并成一条等于什么都没返回 |

`cmd/rag-test` 的 `-dataset-type` 决定用哪一条（留空取 `default`），报告表头与
`report.json` 的 `granularity` 都会写明这次跑的是哪种粒度 —— 同一个 `hits=3`
在两种粒度下不是同一个单位，报告离开当时的上下文就没人知道它是什么。

三条容易踩的边界：

1. **质量指标不跟粒度走。** `Recall@K` / `MRR` / `HitRate@1` 恒按**文档**去重，
   只有结果条数（`hits`、明细行数、`min_results`）跟粒度走。否则「把 chunkSize
   调小」就能把分数刷上去，评测自己就在奖励切得更碎。
2. **`min_results` 比较的是归并后的条数。** 一篇文档命中两块时，`chunk` 粒度下是
   2 条、`document` 粒度下是 1 条 —— 同一条用例在两种粒度下可能一过一不过。语料
   混了好几类库时，**逐个类型各跑一轮再比**，别指望一份数字覆盖两类。
3. **`Chunks` 字段恒为原始命中块数。** 归并到文档之后，明细行数会小于它；差得越
   多说明同一篇文档占掉的名次越多（切块过碎，正在挤掉别的内容）。失败明细里会
   补一行括号说明。

### 指标口径

| 指标 | 算法 | 注意 |
| --- | --- | --- |
| `PassRate` | 通过用例 / 全部用例 | 分母含无答案用例 |
| `Recall@K` | 每条用例 recall 的宏平均 | 分母只含「有期望」的用例 |
| `MRR` | 首个正确结果名次倒数的均值 | 同上 |
| `HitRate@1` | 首个正确结果排第 1 的占比 | 同上 |
| `ACLLeakCount` | 命中 `forbidden_sources` 的条数 | 上限必须是 0 |
| `P50 / P95` | 最近秩法分位数 | 小样本下 P95 一定对应某个真实用例的耗时 |

`Recall@K` / `MRR` / `HitRate@1` 的「命中」都按 **document source 去重**后判定，
与归并粒度无关：一篇文档命中 8 块只算 1 个名次。名次而不是分数决定排序 ——
精确 / 关键词 / 向量三个通道的量纲完全不同，分数不可比，名次可比。

两个刻意选择，改之前先看清代价：

- **命中按 `source` 去重**。一篇文档会切成几十个块，评测问的是「这篇文档有没有被
  找到」，不是「找到几段」。不去重的话，切得越碎名次越多、指标越好看 —— 而
  「把文档切得更碎」正是 RAG 调参里最容易被无意识做的事。
- **无答案用例不进 Recall 分母**。否则多塞几条无答案用例就能把均值"抬"上去，
  指标会随用例集配比漂移，失去区分度。

### 门禁与 CI

`internal/eval/thresholds.yaml` 逐项配门槛，不达标时 `cmd/rag-test` 以非零退出码
结束，可以直接卡在流水线里阻断发布：

```bash
go run ./cmd/rag-test -out logs/eval-report.json || exit 1
```

`-out` 落盘的 JSON 是给机器读的那一份，用来做两次运行的 diff：改切块大小、换
embedding 模型、调 RRF 参数之后的隐性质量回归，就是靠「这一次和上一次差在哪几条
用例」发现的。

阈值怎么定：**先跑一轮看基线，再把门槛压在基线略下方**。凭感觉写一个漂亮数字，
结果只能是长期红灯，红灯久了就没人看了。

### 已知的评测盲区

- **越权（ACL）用例为空**：语料里 `documents.visibility` 全是 `system`，没有私有
  文档，写出来的越权用例没有可验证对象。补一组 private 语料后用精确 source 加
  「属主可读 / 非属主不可读」的对照用例。
- **只评召回，不评回答**：当前没有生成式回答链路，因此无法断言「无答案用例正确地
  没答」。无答案用例现在只验证链路不报错。
- **不覆盖 rerank**：`Reranker` 接口预留了，但 `HybridRetriever` 尚未接收它，
  所以重排前后的排名差异还测不出来。

# 待完善项

本文只记录已确认会影响生产部署或功能边界的事项。RAG 文件扫描、分块、队列驱动的 Embedding 与 Milvus 索引 worker 已实现；其本地验证流程见 [RAG 本地验证](rag-testing.md)。

## P0：检索质量评测与发布门禁

**状态：评测工具已实现，尚未接入流水线**

`cmd/rag-test` + `internal/eval/` 已经能对真实链路跑召回评测：三个召回通道
（`rag.Store` 的精确通道、词法通道与向量通道）+ 加权 RRF 融合（`rag.HybridRetriever`），指标为
Recall@K（宏平均）、MRR、HitRate@1、ACL 泄漏数、P50/P95 延迟，并以
`internal/eval/thresholds.yaml` 判定退出码，可直接用于阻断发布。用例集在
`internal/eval/datasets/`，覆盖产品型号、规格参数、公司信息、跨语言与边界场景。
跑法与口径见 [RAG 本地验证](rag-testing.md) 的「召回评测」一节。

仍未完成的是「持续」这一半：没有任何流水线在执行它，也没有跨版本的指标留档。
另有两块评测盲区，是评测能力本身的缺口而不是流程问题：

1. **越权（ACL）用例为空。** 语料里 `documents.visibility` 全是 `system`，没有私有
   文档，写出来的越权用例没有可验证对象。
2. **无答案准确率测不了。** 没有生成式回答链路，无法断言「无答案用例正确地没答」，
   这类用例现在只验证「检索不报错」。

完成标准：

1. 在 CI 或发布流水线执行 `cmd/rag-test`，以 `thresholds.yaml` 阻断不达标发布。
2. 归档 `-out` 的 JSON 并按 embedding 模型、切块策略、检索配置做跨版本对比，
   避免参数改动造成隐性质量回归。
3. 补 private 语料后的 ACL 对照用例；Rerank 接入后补排名前后对比。

## P0：search_knowledge 尚未接检索

**状态：HTTP 检索端点已接入，对话侧工具仍是空实现**

HTTP 侧已接通：`POST /api/v1/dataset/:id/search`（`SearchDataset`）按 `dataset_id` 限定范围，
走三通道加权 RRF 召回，响应含 `channels`（各通道是否执行/命中数）与 `degraded`（失败通道）
台账，可区分「通道挂了」与「确实没有相关内容」；结果按数据集类型的归并粒度返回
（`knowledge.recallGrouping`，见 [架构](architecture.md) 的「召回结果的归并粒度」）。
装配在 `cmd/restapi/retrieval.go`：Milvus / ES / embedder 任一缺失只关掉对应通道，
全缺才返回不可用。

仍未接的是 `cmd/restapi` 注册的 `search_knowledge` 工具：它是 bindings 版本，只解析主体的
数据集白名单，`run` 方法整段被注释、直接返回空串。这意味着**对话侧目前不会真的召回任何
内容**，Agent 的回答里也不会出现引用 —— 知识库问答这条链路的最后一跳仍然是断的。

接上它需要：

1. 把 HTTP 侧同一个检索器（或直接复用 `application/knowledge.Service.Search`）注入工具，
   `run` 里按解析出的数据集白名单过滤后检索 —— 白名单是强制的，不能让模型通过工具参数
   扩大范围。复用 `Service.Search` 还能顺带拿到按库类型归并好的结果，不用在工具里重做一遍。
2. 把命中结果的 source / heading_path / chunk id 格式化成引用，供回答标注。
3. 用 `cmd/rag-test` 的同一批用例，验证「经工具返回的结果」与「直连检索器」一致，
   确保过滤逻辑加在工具层时不会被绕过。
4. 明确工具侧是否按 `owner` 进一步收窄：HTTP 端点以数据集为授权边界（private 文档的
   owner 过滤留给对话侧），这个策略要随工具一起定下来。

## P1：Rerank 接入

**状态：调用点已就位，但没有实现、也没接线**

`rag.Reranker` 接口、`HybridRetriever.reranker` 字段与融合后的调用点都已经存在
（`internal/rag/retriever.go` 在加权 RRF 融合与质量过滤之后调用它，`HybridConfig.Reranker`
为 nil 时整段跳过）；配置侧的 `retrieval.enableRerank` / `maxRerankCandidates` 也有校验。
缺的是**一个具体实现**和**组合根里的一次注入**：全仓库没有任何非接口的 `Reranker` 实现，
`cmd/restapi` 与 `cmd/rag-test` 都没有把实例传进 `HybridConfig`，所以
`retrieval.enableRerank: true` 目前不改变任何排序结果。

另有一处细节：`maxRerankCandidates` 只在配置校验里被读（要求启用时 > 0），
**没有被检索器消费** —— 也就是「重排前把候选截到 N 条」这层保护还没生效，
接入实现时要一并补上，否则重排成本会随候选集线性膨胀。

完成标准：

1. 实现一个可配置的 reranker 并在组合根注入，在 ACL 过滤后对**受 `maxRerankCandidates`
   约束的**有限候选集重排。
2. 明确超时、降级和错误策略（当前实现是「rerank 报错即整体报错」，需确认是否应降级为
   保留原始 RRF 顺序），并记录 rerank 前后排名以便诊断。
3. 增加重排生效、超时降级和私有文档不泄漏的测试。

## P1：ES 未配置时，精确通道整体缺失、关键词通道搜不到元数据

**状态：降级路径的能力缺口**

三个召回通道里有**两个依赖 ES**，所以「没配 `es.address`」不是少一个通道，而是少两个：

- **精确通道直接消失**：`rag.Store.SearchByExact` 未配 ES 时返回空并把该通道标记为降级。
  型号、系列这类逐字查询失去最强的那条路。
- **关键词通道降级**：配了 ES 走 BM25，可打分字段 = **基础层**（`source`/`title`/
  `heading_path`/`content`/`metadata_text`，代码内置）**加上** mapping 文件
  `configs/es/chunk_mapping.yaml` 里声明的**业务层**字段（`model`、`product_id`、
  `series_name`、`category_l1/l2`、`variants` …，值从分块 `metadata` 列按 `from` 路径提取）；
  没配 ES 则回落 `rag.Store.searchBySubstring`，它只在 `document_chunks.content`、
  `heading_path` 与 `documents.source` 上做词元子串匹配 —— **完全不看 metadata 列**。

两者叠加起来意味着：没有 ES 时，「H105P」「图冠系列」这类只出现在产品块 YAML 头里的查询
两条通道都够不着，只剩向量通道兜底 —— 而向量通道对短型号串的语义相似度往往不足以兜住。
这不是 bug 而是降级路径的能力边界，但它很隐蔽：**症状与「召回质量差」完全一样**，只有
对照 `documents.metadata` 才会发现型号根本没进过检索面。

好在降级是**显式**的：未配 ES 时响应里的 `degraded` 会带上 `exact`，不是静默返回空结果。

完成标准（任选其一）：

1. 在 `searchBySubstring` 的 SQL 里加上 `document_chunks.metadata::text ILIKE ANY(...)`
   —— `document_chunks.metadata` 上已经有 GIN 索引（见 `ent/schema/documentchunk.go`），
   加这一路不会引入新的索引成本，顺带把精确通道的无 ES 兜底也补上；或
2. 把 ES 列为部署的硬依赖，并在 `cmd/rag-test` 的预检里把「没配 ES」从告警升级为失败。

## P1：知识库文件上传与批量导入

**状态：接口已实现，但不接收 multipart**

`/api/v1/dataset/:id/documents` 下的文档增删改查与重建索引已经落地：正文可以直接
放在 JSON 请求体里（`content`），也可以只给 `source` 注册一个已经在 `knowledge.root`
里的文件。索引是异步的，见本文末尾的「文档索引的异步边界」。

仍未实现的是 `multipart/form-data` 上传与目录批量导入：当前没有流式的文件接收
路径，超过 `runtime.maxRequestBodyBytes` 的文档无法通过接口写入，只能先落到
`knowledge.root` 再按 `source` 注册。

完成标准：

1. 提供流式 multipart 接收，把文件直接落进托管目录，避免整个正文进内存。
2. 提供目录级批量导入，按 `knowledge.root` 扫描并幂等登记。
3. 对私有文档保存上传者为 `owner_subject`，并覆盖 ACL 与审计测试。

## P1：Skill 未授予默认 Agent

**状态：设计待决**

启动过程会注册 `list_skills` 和 `load_skill`，但默认专项 Agent 未取得它们。因此 `skills/` 中的内容不会被默认 Agent 实际加载。

完成标准：

1. 明确哪些专项 Agent 可以使用 Skill。
2. 将所需 Skill 工具加入对应的 `registry.Require(...)`。
3. 对受策略保护的 Agent，同步更新 `security.allowedTools`。
4. 添加 Agent 路由测试。

## P2：检索可观测性与全量重建

**状态：文档生命周期已闭环，指标与版本切换缺席**

按文档查询状态、重建索引、删除文档并清理对应向量已经实现（`POST /api/v1/dataset/:id/documents/:docId/reindex`、
`POST /api/v1/dataset/:id/reindex`、`DELETE .../documents/:docId`）。Indexer 会记录
批处理数量与失败原因，但**没有指标**：队列积压、重试/失败数、各检索通道命中数和
检索延迟都只能翻日志。

另一个缺口是 embedding 模型、维度或 Milvus 集合变更时的**整库重建与版本切换**：
现在重建会原地覆盖同一个集合，切换期间新旧向量混在一起，没有灰度和回滚路径。

完成标准：

1. 暴露摄取量、队列积压、重试/失败数、精确/关键字/向量通道命中数（`SearchOutcome.Channels`
   里已经有这些数字，但只随响应返回、没有导出成指标）和检索延迟指标。
2. 为 embedding 模型、维度或集合变更设计全量重建和版本切换流程。

## P2：元数据检索面的已知取舍

**状态：按当前需求够用，以下三项是有意留下、不是遗漏**

检索面由一份 mapping 声明（`configs/es/chunk_mapping.yaml`）驱动：同一处声明同时
决定 ES 索引属性、查询字段权重、以及分块 `metadata` 里哪些路径要抽成独立字段。
字段分两层：**基础层**（`chunk_id`/`source`/`title`/`content` 等，代码内置，所有
部署共用）与**业务层**（型号、系列、分类等，随产品线在配置文件里增减）。以下三项
是有意留下的取舍：

1. **规格明细共享一个权重。** 业务层里显式声明的键（当前是 `model`、`product_id`、
   `product_name`、`series_name`、`family_prefix`、`category_l1/l2`、`variants`）各自
   有独立字段与权重，而 `specs_from_doc` 里的规格（`door_material`、`surface_finish`、
   `open_angle_deg` …）没有在 mapping 里声明，全部摊平进兜底字段 `metadata_text`，
   共享权重 2。想给「表面处理」这类单独加权，**只需在 mapping 里加一行 `from:
   specs_from_doc.surface_finish`**（无需改代码）；代价是改 mapping 会换掉 `mapping_rev`
   指纹（见下），已有文档要 reindex 才会出现在新字段里。等评测显示某一类规格查询
   确实被淹没时再加。
   提成独立字段是**搬家**而不是「再加一份」：取值与摊平是同一次判断的两面
   （`es.Mapping.Project`），声明过的路径不会再进兜底字段。两边各留一份不增加召回
   （同一个词在这篇文档里本来就命中），只会抬高它的 tf —— 而中文业务词（铰链 /
   固装 / 系列名）几乎每篇文档都有、IDF 接近零，tf 的差异反而会让它们主导排序，
   表现是「问固装铰链，结果全被拽到只含铰链的文档上」。
   排除只针对**确实取到值**的路径：路径拼错、该文档没有这个键、数组没声明 `multi`
   时取不到值，那些内容继续留在兜底字段里，不会被静默抹掉。
2. **只索引值、不索引键。** `metadata_text` 里装的是值的拼接（`镀钛镍 冷轧钢 100`），
   键名（`surface_finish`）不进索引 —— 它们是英文标识而不是用户的搜索词。因此按
   键名搜（搜「surface_finish」）不会有结果。显式声明的业务字段同理：`from` 决定取值
   路径，字段名本身不参与匹配，匹配的是该路径下的值。
3. **兜底字段的排除表是白名单式维护的。** `application/knowledge/keyword_doc.go` 里的
   `metadataExcluded` 逐条列出**机制键**（当前是 `doc_id`、`content_hash`、`visibility`、
   `owner`、`source`、`title`、`heading_path`、`chunk_index`、`dataset_id`、`type`）。
   排除的理由分两类：一类是已经在索引里有专门字段（`source`/`title`/`heading_path`），
   摊平一遍只会重复计分；一类是链路自身的机制数据，其中枚举值尤其危险
   —— 把 `visibility=system` 索引进去，搜「system」就会命中全库。**新增一个机制键
   （比如将来给每个分块加 `tenant_id`）时要记得加进去**，否则它会静默进入可检索文本。
   剔除发生在进入 es 包之前，所以 mapping 里的 `from` 只能指向业务键。
   这张表只管**机制键**：业务字段（mapping 里声明了 `from` 的那些）的排除由
   `es.Mapping.Project` 按声明自动完成，不需要在这里登记 —— 两处都登记的话，
   加一个业务字段就得记得改两个地方。

两条与运维相关的：

- **改 `es.analyzer`** 必须同时换 `es.index` 名（mapping 不能就地换分词器）。
  worker 启动时会校验并直接报错，所以不会静默；但要重建索引这一步绕不过去。
- **新增业务字段**（`fields` 里加一条）不需要换 `es.index`：`EnsureIndex` 会原地
  补写 mapping，已有文档 reindex 后才有新字段的值。指纹 `mapping_rev`（对 analyzer +
  字段形状做 SHA256）会随之变化，worker 用它统计「旧形态文档」并告警，`cmd/rag-test`
  预检把这类文档数出来。**权重微调不影响指纹** —— 只改 `boost`/`baseBoost` 属于查询期
  变化，不触发重索引。反过来，**改已有字段的类型**（如 `text` → `keyword`）无法就地
  生效，ES 会拒绝，必须换 `es.index` 名重建。


## 文档索引的异步边界

文档写入路径（创建、更新正文、重建索引、上传）在请求内只做三件事：落正文、
建文档行、往 asynq 的 `index` 队列投一个任务。**切块不在请求内做** —— 一份
文件里有几个产品、每个产品多长，要等 worker 走完 `rag.Pipeline` 的拆分与切块
之后才知道，请求内算不出 `chunk_count`。worker 消费该任务，依次完成拆产品块、
切块、embedding、向量写入，并把 `document.status` 收敛成 `ready`。

由此带来五条运维前提，部署前需要确认：

1. **索引链路多了一个必需依赖：Redis。** 投递失败会让写文档的请求直接报错
   （`asynq.enabled=false` 时同样如此），而不是「先落库、后台慢慢重试」。这是刻意
   的取舍：队列不可用时的静默降级，最终会变成文档卡在 `indexing` 而没人知道。
   Redis 的连通性在进程启动时就探测一次，地址或密码配错当场失败。
2. **worker 是独立进程，必须单独部署。** 它是 `cmd/worker`，消费 `asynq.queues`
   里列出的队列（默认只有 `index`）。`cmd/restapi` 只投递，不消费。
   - `asynq.enabled=false` 时 worker 拒绝启动（没有可消费的队列，空转没有意义）；
     而 HTTP 侧仍然会起来，写文档会在投递那一步报错。
   - 漏配 `asynq.queues` 会让任务投进 `index` 却永远没人领 —— asynq 默认只消费
     `default` 队列，这是个安静的坑，所以配置校验要求 `queues` 非空。
   - worker 的依赖比 HTTP 重：PostgreSQL、Milvus、embedding 服务缺一个都起不来
     （启动即校验，而不是等任务进来才失败）。Elasticsearch 是可选依赖：配了
     `es.address` 会在启动时探活并校验/补写索引，没配就跳过关键词索引这一步。
   - **写 ES 失败会让任务重试**，与写向量失败同等对待：`_bulk` 的部分失败（HTTP 200
     但 items 里带 error）也算失败。这是刻意的 —— 忽略它的后果是「写入报成功，但
     某些分块就是搜不到」。重试按 chunk 收敛，已成功的部分不会重做。
3. **任务以文档为粒度，重试按 chunk 收敛。** 一个坏段落不会让整篇文档白跑：worker
   每次只挑还 `pending` 的 chunk，重试自动跳过上一轮已成功的部分。次数耗尽
   （`asynq.maxRetries`）后，剩下的 chunk 会被标成 `failed`，`document.status` 随之
   变 `failed`，需要人工触发一次 reindex。已经归档的任务留在 asynq 的归档队列里，
   超过保留期会被清理。
   - **幂等靠 `document.metadata.content_hash`。** 只有正文真的变了才重切分块；
     哈希一致时直接跳到补 `pending` 的分块，上一轮已成功的部分不会被推倒重来。
   - **重试耗尽才落终态**，判断依据是从 context 取的 `queue.RetryState`（由 asynq
     适配器注入 `GetRetryCount/GetMaxRetry`）。`ctx` 被取消（进程在退出）时不写
     终态 —— 那会把一次优雅关闭变成用户看到的「索引失败」。
4. **删除路径上的向量清理是同步且尽力而为的。** Milvus 不参与关系库事务，
   清理失败只记警告：孤儿向量取不回来（检索要回到 chunk 行做过滤），但会一直
   占着向量库空间，需要靠后续的全量重建收拾。HTTP 进程连不上 Milvus 时也能启动，
   退化成「只摘索引不删向量」。
5. **`chunk_count` / `indexed_chunk_count` 是实时聚合出来的，不是列。** ent 的
   `Document` 上没有这两个字段，接口每次从 `document_chunks` 分组统计。切块在
   worker 内完成，请求返回时给不出准确值，维护冗余列只会让它慢慢漂。

## P1：产品拆分目前只认 front-matter（待替换为 LLM 拆分）

**状态：接缝已就位，实现是默认桩**

摄取链路的形状是「一份文件 → N 个产品块 → 每块再切 chunk」，这两步都在
`rag.Pipeline` 的 ingest 链里完成（`cmd/worker` 与 `cmd/ragserver` 共用同一份
实现）。拆产品的接缝就是 eino 原生的 parser 接口，在 `FileLoader` 里选实现：

```go
type Parser interface {
    Parse(ctx context.Context, reader io.Reader, opts ...parser.Option) ([]*schema.Document, error)
}
```

默认实现是 `rag/parser.ProductParser`，它按正文里的 `---` front-matter 分节 ——
因为眼下上传的文件本来就是 LLM 生成好的、带分节结构的 Markdown，这个默认桩能
直接跑通全链路。目标形态是换成 LLM 拆分：把一份混合了多个产品的资料丢给模型，
由模型判定「这是几个产品、每个产品的边界在哪」。

**替换点只有一处**：`internal/rag/loader.go` 的 `NewFileLoader` 里那张注册表
（`parser.NewParser` 的 `Parsers` map）。`cmd/worker` 与 `cmd/ragserver` 用的是
同一个 `FileLoader`，所以换一次两边同时生效；`knowledge.Indexer` 与传输层都不用动。

改这张注册表时有个必须守住的前提：**注册用的键、查表时读的键、调用方传的键，
得是同一个**。眼下三者是这样连的：`loader.go` 注册 `"product" → ProductParser{}`；
`knowledge.Indexer.ingest` 用 `parser.WithExtraMeta{"type": ...}` 传键，值是
`doc.Edges.Dataset.Type`；`rag/parser.Parser.Parse` 读 `ExtraMeta["type"]`。

**这里有一个静默失效的坑**：`dataset.type` 的 schema 默认值是空串，空串查不到注册表
里任何键，于是分发落到 fallback 的 `parser.TextParser{}` 上 —— 不报错、不告警，结果
整篇文档被当成一个块切，`product_id` / `model` / `specs_from_doc` 全部缺失，检索侧的
型号过滤随之失效。所以**建数据集时必须显式传 `type=product`**（`CreateDatasetReq.Type`
会落库），否则产品拆分等于没接。排查看 `knowledge.ingest_document` span 上的
`knowledge.dataset_type` 与 `knowledge.parsed_blocks`（退化时恒为 1）。

同源的两条待收：`typ.(string)` 是无保护断言，**调用方完全不传 `type` 就会 panic**
（`cmd/ragserver` 的 `IngestFile` 正是这种调用方式）；空串落到 `TextParser` 属于静默
降级，与本条第 3 个约束（失败要显式报错）直接冲突 —— 查不到的键应当报错，或者至少
要在日志里看得见。

替换时要守住三条约束，否则下游会以很难查的方式坏掉：

1. **输出契约不变。** 仍然返回 `[]*schema.Document`，`MetaData` 要继续携带
   `product_id` / `model` / `specs_from_doc` / `variants` 这组键（`_source` 由
   loader 通过 `parser.WithURI` 写入，`Indexer` 会把它统一成 `documents.source`），
   否则 chunk 元数据与检索侧的型号过滤条件会对不上。
2. **必须可重放。** worker 的重试是「按 chunk 收敛」的，同一个任务可能被投第二次。
   拆分结果要由内容决定而不是由调用次数决定（同一份正文两次 Parse 必须得到同样的
   块数与边界），否则 `content_hash` 的幂等会被绕过，chunk 行每轮都重新洗牌。
3. **失败要显式报错。** 拆不出来（模型超时、返回不可解析）就返回 error，让任务走
   重试并最终落到 `document.status=failed`。不要「拆失败就退化成整篇当一个块」——
   那会静默产出一个检索质量很差但看起来成功的索引。

另外，`Parse` 在 worker 进程内调用，所以新增的模型调用会占用
`asynq.shutdownTimeoutSeconds` 的排空预算；`rag.Pipeline` 的 `Config.Timeout`
（当前走默认 60s）也要一并放大，替换后这两个值都要重新评估。

## P1：生产数据库迁移策略

**状态：开发模式实现**

`internal/platform/storage/entx/database.go` 会在启动时调用 `client.Schema.Create`。这适合学习和本地开发；生产环境应采用受版本控制的 Ent/Atlas migration，避免多实例启动时改表。

## P2：运行状态与可观测性闭环

**状态：链路已通，验证与配置收口未完成**

运行、审批、对话轮次和 tracing 已有基础实现，但仍缺少覆盖中断、恢复、异常和超时路径的端到端测试。

链路追踪的具体形状见 [架构与运行说明](architecture.md) 的「链路追踪」一节。当前遗留：

- **配置源分叉。** HTTP 进程的 trace 服务名来自 `internal/transport/restapi/etc/restapi.yaml`
  的 `Telemetry.Name`，worker 的来自 `configs/config.yaml` 的 `observability.workerServiceName`，
  而同一份业务配置里的 `observability.serviceName` 对 trace 已经不起作用（只有 worker 的
  缺省值会用到它）。三处要一起改才一致，很容易只改一处。
- **缺端到端断言。** `internal/platform/queue/asynq/trace_test.go` 覆盖了信封编解码、旧
  payload 容错与「消费端 trace id 等于投递端」，但「HTTP 的 server span 与 worker 的
  consumer span 落在同一条 trace 上」还没有自动化验证 —— 那需要一个真实 Redis 与一个
  能收 span 的 collector。改动 `platform/queue/asynq` 的注入/提取逻辑后，建议手工跑一遍
  `docs/rag-testing.md` 的上传流程，在 trace 界面上确认两个服务名出现在同一条 trace 下。
- **读路径没有埋点。** `Service.Get/List/ChunkStats` 只有 HTTP 层那一个 server span，
  没有自己的 span；写入与索引路径是重点，读路径暂时靠 HTTP span 兜着。

# 数据集组待实现的接口与字段

驱动来源：前端 agent-platform 的知识库（dataset）改造。前端已按下面的形态写完并调用，
后端就位后不需要再改前端（路径/字段名都在前端的常量里集中定义，见文末）。

核对基准：`docs/dataset/dataset.api`、`internal/types/types.go`、`ent/schema/dataset.go`、
`internal/logic/dataset/*`（git `e4569e6`）。

---

## 0. 现状核对

| 项 | 契约声明 | ent 落库 | 前端已对接 | 状态 |
| --- | --- | --- | --- | --- |
| `DatasetResp.type` | ✅ | ✅ `field.String("type").Default("")` | ✅ | 已完成 |
| `CreateDatasetReq.type` | ✅ | ✅ `SetType(...)` | ✅ | 已完成 |
| `DatasetDTO` 映射 type | — | — | ✅ | 已完成 |
| 11 条 `/dataset*` 路由 | ✅ | ✅ | ✅ | 已完成 |
| `ListDatasetsReq.type` | ✅ optional query | ❌ logic 完全忽略 | — | **半成品** |
| `POST /dataset/:id/documents/upload` | ❌ | — | ✅ 已调用 | **待实现** |
| `GET /dataset/types` | ❌ | — | ✅ 已调用（带兜底） | **待实现** |
| `GET /dataset/models` | ❌ | — | ✅ 已调用 | **待实现** |
| `embedding_model` / `text_model` / `image_model` | ❌ | ❌ | ✅ 已对接 | **待实现** |
| 上传文件的正文提取 | — | ❌ | 依赖 | **待实现** |

`type` 字段已经打通（contract → ent → logic → DTO 四处齐全），无需再动。

---

## 1. 待新增接口（3 条）

### 1.1 `POST /api/v1/dataset/:id/documents/upload`

界面**唯一**的建文档入口。原来的 JSON 建文档（`content` 必填、`source` 可选）保留但界面不再使用
—— 写正文的入口已从界面移除，正文提取全部归服务端。

**请求**：`Content-Type: multipart/form-data`，`group: dataset`，`middleware: RoleAdmin`

| 表单项 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `file` | file | 是 | 原始文件字节。正文由服务端从文件提取，前端不读不猜 |
| `title` | string | 否 | 空串则从文件名推断 |
| `visibility` | string | 否 | `system` / `private`；空串表示跟随默认值 |

三个字段前端**始终发送**（哪怕是空串）。这是刻意的：go-zero 的 `core/mapping` 只认 `optional`
标签、不认 `omitempty`，标量缺席直接 400 `field "x" is not set` —— 这条已在 JSON 建文档路由上实测过。

**响应**：`200 DocumentResp` —— **不新增任何字段**，复用现有类型。

`status` 是 `indexing`（切块由后台 worker 完成，返回时 `chunk_count` 还是 0，它会随 worker
推进而增长；embedding 再追 `indexed_chunk_count`）。客户端不要用 `status != ready` 判失败。

**错误**：`400` 没带文件 / 文件解析不出正文 / 不支持的格式；`404` dataset 不存在；
`413` 超过正文上限（`knowledge.maxDocumentBytes`，5 MiB）。

**goctl 落地要点**（已核对 `tools/goctl@v1.10.0` 源码）：

goctl **没有** `file` / multipart 的内置类型 —— `parser` 里没有 `file` 类型，模板里也没有
`ParseMultipartForm`（grep 无命中）。所以文件那一行必须手写。两条路：

- **A（推荐）**：在 `dataset.api` 里正常声明路由，请求类型只带 `path` + 带 `form:` 标签的标量字段：
  ```
  type UploadDocumentReq {
      ID         uint64 `path:"id"`
      Title      string `form:"title,optional"`
      Visibility string `form:"visibility,optional"`
  }
  ```
  生成的 handler 里手写 `r.FormFile("file")`，把 `*multipart.FileHeader` 穿给 logic。
  标量字段能自动绑：go-zero 的 `httpx.Parse` 对 `form:` 标签会调 `r.ParseMultipartForm`
  （见 `rest/httpx/util.go` 的 `GetFormValues`），所以不用自己再 parse 一遍。
  路径与 RoleAdmin 中间件仍由生成器管。

- **B**：完全不进 `.api`，照两条 SSE 路由的先例在 `restapi.go` 里
  `server.AddRoute(rest.Route{...}, ...)` 并挂 `serverCtx.RoleAdmin(...)`。适合逻辑很重、
  不想被生成器约束的场合。

**顺带清理**：`restapi.go` 第 34 行注释里的 "the upload placeholder's 202" 是**过期注释**，
现在没有 upload 路由。落地时一并删掉，免得下次又按它去找一条不存在的路由。

---

### 1.2 `GET /api/v1/dataset/types`

知识库类型下拉的可选项。`value` 落库、`label` 上屏。**无请求参数。**

推荐形态：

```json
{ "data": [ { "value": "product", "label": "产品知识" }, { "value": "qa", "label": "Q&A知识" } ] }
```

也兼容纯字符串数组 `{ "data": ["product", "qa"] }`（前端用预设表把 value 翻成中文）。

前端有**双重兜底**：请求失败（404）或返回空数组，都会退回预设的
产品知识 / Q&A知识 / 销售知识三项。所以这条**可以先返回空数组上线**，不会阻塞建库，
后续接上配置表即可 —— 新增一种类型只改后端，前端下拉自动出现。

路由安全性：静态段 `types` 与参数段 `/:id` 同层共存、静态优先（本仓库已在
`/documents/upload` 上实测过，见 `routes_test.go` 的说明）。

---

### 1.3 `GET /api/v1/dataset/models`

解析/索引模型目录，一次回全部用途，用 `kind` 区分。**无请求参数。**

```json
{ "data": [
  { "value": "text-embedding-v3", "label": "text-embedding-v3", "kind": "embedding" },
  { "value": "qwen-plus",         "label": "qwen-plus",         "kind": "text" },
  { "value": "qwen-vl-plus",      "label": "qwen-vl-plus",      "kind": "image" }
] }
```

- `kind` 只认 `embedding` / `text` / `image`，**其余取值前端直接丢弃**（不猜用途 —— 猜错会把
  索引模型放进图片模型里）。
- `value` 为空串的项也丢弃。
- `label` 缺失时退回 `value`（模型名本身就是最好的展示名）。
- 同一个模型既可用于文本也可用于图片 → **按 `kind` 出现两次**。

⚠️ **这条没有兜底**。和类型选项不同，模型没有"预设表"可退 —— 编几个假模型名让用户选，
比看到"列表加载失败"糟得多：选中那个可能不存在，索引跑不起来，而且**创建后不可修改**，
只能删库重来。所以后端落地前，建库表单的三个下拉是**禁用态**（仍允许创建，字段发空串，
由服务端取默认值）。

---

## 2. 待新增字段（3 个，都在 dataset 表）

| 字段 | 类型 | 默认 | 语义 |
| --- | --- | --- | --- |
| `embedding_model` | string | `""` | 索引模型：正文切块后写向量用的那个 |
| `text_model` | string | `""` | 文本理解模型：语义理解 / 摘要等文本侧处理 |
| `image_model` | string | `""` | 图片理解模型：文档内图片的识别与描述 |

**这四项配置挂在知识库级，不在文档级** —— 该库下的所有文档都按这三项 + `type` 处理，
文档上不带任何模型或解析配置。前端上传弹窗里只有标题和可见性。

改动点共 4 处，逐一对齐：

1. **`ent/schema/dataset.go`** —— 照 `type` 的写法加三行：
   ```go
   field.String("embedding_model").Default(""),
   field.String("text_model").Default(""),
   field.String("image_model").Default(""),
   ```
   然后重新生成 ent（`make gen-ent` 或项目对应目标）。

2. **`docs/dataset/dataset.api`** —— `CreateDatasetReq` 与 `DatasetResp` 各加三个字段，
   字段名不带 `optional`（与现有 `Type string \`json:"type"\`` 保持一致；前端始终发送）。

3. **`internal/logic/dataset/create_dataset_logic.go`** —— 照 `SetType(strings.TrimSpace(req.Type))`
   加 `SetEmbeddingModel(...)` / `SetTextModel(...)` / `SetImageModel(...)` 三行。

4. **`internal/logic/dataset/helpers.go` 的 `DatasetDTO`** —— 加三个映射
   （`EmbeddingModel: base.EmbeddingModel` 等）。

**生成顺序**：改 `.api` → **先删掉** `internal/types/types.go` 和
`internal/handler/dataset/create_dataset_handler.go`（goctl 不覆盖已存在的文件，字段变了会
编译报"参数不匹配"）→ `make gen-api`。

⚠️ **不需要任何更新端点**。三项是「创建时确定、之后不可修改」的产品约定，
前端不发写请求，详情页只读展示。**不要**加 PUT / PATCH —— 加了也没有消费者。

---

## 3. 连带项（不改就会"白改"）

### 3.1 MaxBytes 必须调大 —— 关键，否则上传必然 413

`internal/transport/restapi/etc/restapi.yaml` 现在是：

```yaml
MaxBytes: 5242880   # 正好 5 MiB
```

而前端 `MAX_UPLOAD_BYTES` 也是 5 MiB。go-zero 的 MaxBytes 中间件按**整个请求体**计数，
multipart 还要额外叠加 boundary、`title` / `visibility` 字段和文件名 —— 一个 5 MiB 的
文件必然超过 5242880，被直接拒掉。

→ 把 `MaxBytes` 提到 **≥ 16 MiB**（或按产品实际上限定，留足 multipart 开销）。

另：`ParseMultipartForm` 的 maxMemory 在 go-zero 里固定 32 MiB
（`rest/httpx/requests.go:20`），超过会落临时文件，够用，不用管。

### 3.2 `ListDatasetsReq.type` 是死参数

契约声明了 `Type string \`json:"type,optional"\``，但 `list_datasets_logic.go` 只做
`Query().Order(dataset.ByID()).All()`，**完全没使用它** —— 传了不生效。

要么实现（`Where(dataset.TypeEQ(strings.TrimSpace(req.Type)))`，注意空串要跳过），
要么从契约里删掉这个字段。别留着"声明了但不生效"的参数，它比没有更误导。

### 3.3 正文提取能力（服务端的隐藏工作量）

界面已经没有"写正文"入口，`content` 那条 JSON 路径成了死路。服务端要能：

- 按 MIME / 扩展名分流（文本类直读；PDF / Office 走解析器）
- 编码识别（GBK → UTF-8 之类）
- 结果写进 `documents.content`（服务端已经不再有托管目录，见 `docs/architecture.md`
  的「知识库数据流」），`documents.source` 只是按 `datasetId + 标题` 派生的逻辑标识
- 复用现有的正文上限（`knowledge.maxDocumentBytes`，默认 5 MiB），超限的状态码要和前端文案对上

前端 `UPLOAD_ACCEPT` 常量里列的格式是**期望**能解析的清单（md / txt / csv / json / yaml /
xml / html / pdf / doc(x) / xls(x) / ppt(x)）。服务端如果暂时只收文本类，把这个常量收窄即可 ——
前端不按扩展名拦截，那只是系统文件选择框的默认过滤。

---

## 4. 前端对应文件（后端改路径 / 字段名时只需改这几处）

| 要改的东西 | 位置 |
| --- | --- |
| 类型选项路径 | `src/api/dataset.ts` → `DATASET_TYPE_PATH` |
| 模型目录路径 | `src/api/dataset.ts` → `DATASET_MODEL_PATH` |
| 上传路径 | `src/api/dataset.ts` → `documentsUploadPath` |
| 上传表单字段名 | `src/api/dataset.ts` → `UPLOAD_FIELD_FILE` / `_TITLE` / `_VISIBILITY` |
| 契约字段名 | `src/api/model/dataset.ts` → `DatasetResp` 的 `type` / `embedding_model` / `text_model` / `image_model`、`ModelOptionItem.kind` |
| 大小上限与格式白名单 | `src/api/model/dataset.ts` → `MAX_UPLOAD_BYTES` / `UPLOAD_ACCEPT` |

---

## 5. 验收方式（三条就能验完）

1. **建库 + 三模型落库**：`POST /api/v1/dataset` 带 `type` 与三个 `*_model` →
   `GET /api/v1/dataset/:id` 原样读回。
2. **上传**：`curl -F file=@a.md -F title= -F visibility= 'http://127.0.0.1:8090/api/v1/dataset/1/documents/upload'`（记得带 admin Bearer）
   → 200 `DocumentResp`、`SELECT length(content) FROM documents WHERE id=1` 大于 0；
   再用**接近 5 MiB** 的文件重试一次，确认没被 MaxBytes 中间件打回 413。
3. **两个目录接口**：`GET /dataset/types` 与 `GET /dataset/models` 都返回 `{"data":[...]}`，
   前端建库弹窗里类型下拉有选项、三个模型下拉从禁用变可用。

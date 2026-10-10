# P10 文档启停验收说明

> 目标：把文档启停从“每次请求翻转一次”改成“设置明确目标状态”，让网络重试不会把状态改回去。
>
> 当前状态：**本地契约验收通过；真实 HTTP 进程重启和外部依赖联调待复测。**

## 1. 交付内容

- API 请求必须携带 `enabled`：`true` 表示启用，`false` 表示停用。
- 缺失 `enabled` 时返回 `400 bad_request`，不会默认成 `false`，也不会执行隐式翻转。
- 业务层用 `datasetID + documentID` 联合定位文档；把一个数据集下的 `docId` 放到另一个数据集路径下不能修改文档。
- 相同目标值重复提交是幂等操作：连续提交 `false` 仍是停用，连续提交 `true` 仍是启用。
- 停用只改变召回可见性，不删除正文、分块或索引数据；启用后恢复召回。

## 2. 自动验收

在仓库根目录执行：

```bash
bash scripts/acceptance/run.sh P10
```

通过时会同时运行：

1. 应用服务状态机测试：连续启停、跨数据集父子路径保护、正文保留、停用过滤和重新启用后的召回。
2. HTTP handler 契约测试：缺失字段返回 400、显式停用成功、重复停用不翻转。

报告目录会打印在终端，并包含：

- `tests.log`：完整测试输出；
- `summary.json`：机器可读的验收摘要。

## 3. 手工 HTTP 验证

假设已有：

- `DATASET_ID`：目标数据集 ID；
- `DOC_ID`：该数据集下的文档 ID；
- `BASE_URL`：API 地址，例如 `http://127.0.0.1:8090`；
- 可用的管理员认证头。

### 3.1 缺失目标值必须拒绝

```bash
curl -i -X PATCH "$BASE_URL/api/v1/dataset/$DATASET_ID/documents/$DOC_ID/enabled" \
  -H 'Content-Type: application/json' \
  -H "$AUTH_HEADER" \
  -d '{}'
```

预期：HTTP `400`，错误码为 `bad_request`，文档状态不变。

### 3.2 连续停用不能翻转

```bash
for i in 1 2; do
  curl -i -X PATCH "$BASE_URL/api/v1/dataset/$DATASET_ID/documents/$DOC_ID/enabled" \
    -H 'Content-Type: application/json' \
    -H "$AUTH_HEADER" \
    -d '{"enabled":false}'
done
```

预期：两次都成功，返回的 `enabled` 都是 `false`；搜索固定关键词不能召回该文档，但读取正文仍能得到原文。

### 3.3 连续启用不能翻转

```bash
for i in 1 2; do
  curl -i -X PATCH "$BASE_URL/api/v1/dataset/$DATASET_ID/documents/$DOC_ID/enabled" \
    -H 'Content-Type: application/json' \
    -H "$AUTH_HEADER" \
    -d '{"enabled":true}'
done
```

预期：两次都成功，返回的 `enabled` 都是 `true`；再次搜索固定关键词时文档恢复可召回。

### 3.4 父子路径保护

使用另一个 `OTHER_DATASET_ID`，但仍传入同一个 `DOC_ID`：

```bash
curl -i -X PATCH "$BASE_URL/api/v1/dataset/$OTHER_DATASET_ID/documents/$DOC_ID/enabled" \
  -H 'Content-Type: application/json' \
  -H "$AUTH_HEADER" \
  -d '{"enabled":false}'
```

预期：返回资源不存在或父子关系不匹配错误；原数据集下的文档状态保持不变。

## 4. 真实环境补验

本地验收通过后，还需要在隔离环境补做：

1. 启动 API、PostgreSQL、Redis、worker 和实际检索依赖。
2. 通过真实 HTTP 完成 3.2、3.3、3.4。
3. 停止并重启 API，再读取文档状态，确认数据库中的 `enabled` 值保持不变。
4. 记录真实报告，不把本地 Ent SQLite 和协议测试当成外部依赖端到端结果。

## 5. 相关实现

- API 契约：`internal/transport/restapi/docs/dataset/dataset.api`
- HTTP handler：`internal/transport/restapi/internal/handler/dataset/enable_document_handler.go`
- HTTP logic：`internal/transport/restapi/internal/logic/dataset/enable_document_logic.go`
- 应用服务：`internal/application/knowledge/service.go`
- 应用状态机测试：`internal/application/knowledge/lifecycle_test.go`
- HTTP 契约测试：`internal/transport/restapi/internal/handler/dataset/contract_test.go`

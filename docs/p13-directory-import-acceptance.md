# P13 目录批量导入与增量重跑验收

日期：2026-10-07

## 交付物

- 入口：`go run ./cmd/knowledge-import`
- 用例层：`internal/application/knowledge/directory_import.go`
- 本地验收：`bash scripts/acceptance/run.sh P13`
- 报告：`logs/acceptance/P13/<时间>-<pid>-local/summary.json`
- 固定夹具：`tests/acceptance/p13/`

导入只读取 `--root` 目录及其子目录，默认支持 `.md`、`.markdown`、`.txt`、`.text`。软链接会先解析目标；目标在根目录外时，该文件记为 `failed/path_outside_root`，不会读取外部内容。

## 用户直接验证

### 1. 准备数据集和目录

```bash
export ROOT=/absolute/path/to/tests/acceptance/p13
export DATASET_ID=2
export CFG=configs/acceptance/api.yaml
```

确保 `DATASET_ID` 已存在，并且 API 配置中的 PostgreSQL 可用。正式导入还需要 `asynq.enabled=true`、Redis 可用；worker 由另一进程消费索引任务。

### 2. dry-run：确认只计划、不写库

```bash
go run ./cmd/knowledge-import \
  -config "$CFG" -root "$ROOT" -dataset "$DATASET_ID" \
  -dry-run -report /tmp/p13-dry-run.json
cat /tmp/p13-dry-run.json
```

第一次应看到 3 个 `created`（如果目标数据集已有同 source，则按实际存量显示 `unchanged`）。检查数据库文档数量和 `updated_at` 没有变化。

### 3. 正式导入和原样重跑

```bash
go run ./cmd/knowledge-import \
  -config "$CFG" -root "$ROOT" -dataset "$DATASET_ID" \
  -report /tmp/p13-import.json

go run ./cmd/knowledge-import \
  -config "$CFG" -root "$ROOT" -dataset "$DATASET_ID" \
  -report /tmp/p13-replay.json
```

第一次每个文件应为 `created`；worker 完成后文档进入 `ready`。第二次相同目录应为 `unchanged`，不新增文档，也不重新排队未变化的正文。`source` 应保持不变。

### 4. 只修改一个文件

修改 `nested/second.md` 后再次导入：

```bash
go run ./cmd/knowledge-import \
  -config "$CFG" -root "$ROOT" -dataset "$DATASET_ID" \
  -report /tmp/p13-update.json
```

应看到一项 `updated`、其余项 `unchanged`，且 `updated` 项的 `document_id` 不变。删除目录中的一个文件再导入时，报告的 `missing_not_deleted` 增加，但数据库中的文档不会被默认删除。

### 5. 失败清单和仅重试失败项

把一个文件清空，或放入根目录外的软链接：

```bash
go run ./cmd/knowledge-import \
  -config "$CFG" -root "$ROOT" -dataset "$DATASET_ID" \
  -report /tmp/p13-failed.json || true
```

其他文件仍会继续处理；失败项有 `status=failed` 和明确 `error`。修复文件后只重试失败项：

```bash
go run ./cmd/knowledge-import \
  -config "$CFG" -root "$ROOT" -dataset "$DATASET_ID" \
  -retry-report /tmp/p13-failed.json \
  -report /tmp/p13-retry.json
```

`--retry-report` 中的路径会再次经过根目录边界校验，不能借此读取根目录外文件。

## 边界和未替代项

- 目录导入会复用 `knowledge.Service.Create/Update` 和既有索引队列；它不会自行消费 worker。
- `dry-run` 仍会查询数据库，以便准确区分新增、更新和不变，但不调用写入或排队方法。
- `source` 由数据集 ID、相对路径主干和路径哈希共同生成，改正文不改变引用。
- 不提供默认删除语义；源文件消失需要单独走文档删除接口/运维流程。
- 真实 PostgreSQL、Redis、worker、embedding 和向量库联调仍需在目标环境运行上述手工步骤并留档。

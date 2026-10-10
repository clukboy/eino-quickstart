# P11：无 ES 结构化精确检索验收

> 目标：关闭 Elasticsearch 时，型号/产品 ID 等已在 mapping 中声明为 `keyword` 的字段，仍可在 PostgreSQL metadata 上执行真正的相等匹配；不能把 PostgreSQL 的子串检索冒充 exact。

## 一、自动化验收

在仓库根目录执行：

```bash
bash scripts/acceptance/run.sh P11
```

通过后查看最新报告目录：

```text
logs/acceptance/P11/<时间>-<pid>-local/
├── tests.log
└── summary.json
```

本地测试覆盖：

- mapping 是 ES 与 PostgreSQL 精确回落共用的字段声明；
- 大小写、首尾空格和连续空格按同一规则规范化；
- 无 ES 时完整 `ACC-H105P` 只命中自身；
- `ACC-H105` 只可能由 keyword 子串通道命中，不能出现在 exact 命中中；
- 不存在的完整型号无 exact 命中；
- disabled 文档和其他 dataset 被排除；
- exact 候选进入融合，并在报告中记录为 `exact`；
- `variants` 等多值字段按 mapping 的 `multi` 声明比较。

## 二、用户直接验证步骤

### 1. 配置关闭 ES

复制一份 API 配置，保留 mapping 文件但清空 ES 地址：

```yaml
es:
  address: ""
  mappingFile: "configs/es/chunk_mapping.yaml"
```

确认 API 使用的是这份配置，且 PostgreSQL 中已有两个启用文档，metadata 至少包含：

```json
{"model":"ACC-H105P","product_id":"ACC-H105P"}
{"model":"ACC-H105PE","product_id":"ACC-H105PE"}
```

### 2. 搜索完整型号

查询：`ACC-H105P`

预期：

- 结果中包含 `ACC-H105P` 对应文档；
- 通道台账中 `exact` 有候选；
- `ACC-H105PE` 不能因为前缀相同而成为 exact 命中；
- 重复空格和大小写变化（例如 `  acc-h105p  `）仍命中同一文档。

### 3. 搜索部分型号和不存在型号

依次查询：

- `ACC-H105`：允许 keyword 子串通道返回相关结果，但 exact 不应返回 `ACC-H105P` 或 `ACC-H105PE`；
- `ACC-H999`：exact 和 keyword 都应无相关命中。

### 4. 验证过滤和状态

- 将 `ACC-H105P` 文档设置 `enabled=false`，再次搜索完整型号，结果中不得出现该文档；
- 将它重新设置 `enabled=true`，再次搜索，exact 恢复；
- 使用另一个 dataset 的同型号文档，带 dataset 范围搜索时不得跨数据集返回。

## 三、边界说明

- PostgreSQL exact 回落为了保证正确性会扫描过滤范围内的 metadata；它是无 ES 部署的功能回落，不等价于 ES keyword 查询的规模性能。
- 结构化字段的范围由 `configs/es/chunk_mapping.yaml` 中 `keyword: true` 的字段决定，不在 PG 代码中另写 `model/product_id` 清单。
- mapping 加载失败时 exact 回落关闭；数据库查询失败时应进入检索报告的降级台账，而不能静默改走 substring。
- 真实 API、PostgreSQL 重启恢复以及大规模性能尚需在隔离依赖环境中复测；本地报告不能替代真实联调证据。

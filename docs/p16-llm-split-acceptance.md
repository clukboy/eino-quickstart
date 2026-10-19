# P16｜LLM 产品拆分本地验收

日期：2026-10-07

## 已交付

- `internal/rag/parser/llmsplit` 定义 provider-neutral HTTP 请求/响应契约。
- 响应写库前校验：产品数上限、正文非空、`product_id`/`model` 稳定键、重复键。
- 拆分器由组合根注入（`knowledge.ServiceDeps.ProductSplitter` / `ProductSplitterVersion` / `ProductSplitterModel`）。原计划的 `Knowledge.ProductSplitEnabled`、endpoint、model、解析版本、超时和数量上限等配置键**没有**落到 `internal/platform/config`；验收模板里残留的死键已在 2026-10 的配置迁移中删除，`cmd/restapi` 当前也未注入拆分器。
- 无 front-matter 的模型结果复用现有产品文档写入/索引队列链路；模型失败不会静默退回整份“成功”文档。

## 用户直接验证

```bash
cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2
GOCACHE=/tmp/eino-go-cache bash scripts/acceptance/run.sh P16 --local
```

报告位于 `logs/acceptance/P16/<时间>-<pid>-local/`，重点看 `tests.log` 和 `summary.json`。

真实环境还需：准备双产品原文、人工金标和不可拆分样例；配置 API 拆分服务后上传，逐条检查文档正文、型号和元数据。当前本地通过不代表模型质量已验收。

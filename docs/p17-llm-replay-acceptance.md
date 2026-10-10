# P17｜LLM 拆分重放与并发幂等本地验收

日期：2026-10-07

## 已交付

- 输入正文、拆分模型和解析器版本参与输入指纹。
- 解析版本、输入摘要持久化在产品元数据，版本变化不会静默复用旧边界。
- 产品 `external_key` 继续由 `product_id` 优先、`model` 回退；已有唯一约束冲突重查保护并发写入。
- 同一产品正文/元数据不变时走 unchanged；真正变化或版本变化才更新并重新排队。

## 用户直接验证

```bash
cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2
GOCACHE=/tmp/eino-go-cache bash scripts/acceptance/run.sh P17 --local
```

报告位于 `logs/acceptance/P17/<时间>-<pid>-local/`。

真实环境还需用 API、PostgreSQL、Redis、worker 做串行三次、并发两次、进程重启后重传，并核对文档 ID、数量、指纹和索引任务数；这些不在本地纯契约报告中冒充完成。

# P24 维护清理 dry-run 验收

## 已交付

新增 `cmd/maintenance` 独立入口和 `maintenance.Worker.Preview`：dry-run 只调用计数查询，不执行删除或状态修改；正式执行返回审批过期、审批删除、checkpoint 删除和 turn 删除的逐类计数。持久化 store 增加对应只读计数接口。维护周期、保留期和批大小现在明确属于 worker 配置；`-periodic` 会先执行一次，再按 `maintenance.cleanupIntervalSeconds` 周期执行，并持续写入最新报告。

## 本地直接验证

```bash
cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2
bash scripts/acceptance/run.sh P24
```

## 真实数据库验证

```bash
go run ./cmd/maintenance -config configs/worker/config.yaml -dry-run -report /tmp/maintenance-dry-run.json
go run ./cmd/maintenance -config configs/worker/config.yaml -report /tmp/maintenance-run.json
go run ./cmd/maintenance -config configs/worker/config.yaml -periodic -report /tmp/maintenance-periodic.json
```

先核对 dry-run 报告且确认数据未变化，再执行正式清理。正式入口复用 worker 配置中的周期、保留期和批大小；`-periodic` 适合交给 supervisor/systemd 管理，收到 SIGINT/SIGTERM 会优雅退出。不要在生产库临时缩短保留期做实验。重复执行应得到 0 或更少的待处理项，活跃审批、未过期 checkpoint 和未终态 turn 不应被清理。

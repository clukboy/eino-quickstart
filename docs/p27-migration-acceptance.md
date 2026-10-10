# P27 版本化迁移与安全启动验收

## 已交付的本地边界

- `/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2/internal/platform/storage/entx/migrations/` 保存带版本号的迁移 SQL。
- `schema_migrations` 记录已执行版本；`entx.Status` 可查询，`entx.Migrate` 使用 advisory lock 并可重跑。
- `cmd/db-migrate` 提供显式 `-dry-run`、`-apply`、`-status`。
- 应用进程统一使用 `entx.OpenWithoutMigration`；开发便利入口单独命名为 `OpenDevelopment`。
- 生产连接路径不传 `WithDropColumn` 或 `WithDropIndex`。

## 本地直接验证

```bash
cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2
bash scripts/acceptance/run.sh P27

# 只看计划，不连接数据库、不执行 DDL
go run ./cmd/db-migrate \
  -config configs/worker/config.yaml \
  -profile worker \
  -dry-run
```

## 真实验收（隔离数据库，不要使用生产库）

```bash
# 先查看版本状态，再显式执行迁移，最后复查状态
go run ./cmd/db-migrate -config configs/acceptance/worker.yaml -profile worker -status
go run ./cmd/db-migrate -config configs/acceptance/worker.yaml -profile worker -apply
go run ./cmd/db-migrate -config configs/acceptance/worker.yaml -profile worker -status
```

验收要求：

1. 空数据库首次 `-apply` 成功，重复执行不重复应用版本。
2. 含合成用户、文档、会话历史的副本升级后，历史数据仍可读取；`0002_user_nickname_backfill` 只修复空昵称。
3. 启动两个 API/worker 实例时不会因为应用启动自动执行 DDL；迁移只由显式命令执行。
4. 备份恢复在副本完成后，再执行 `-status`/`-apply`，记录恢复前后版本和数据校验结果。

## 当前边界

本地验收证明的是迁移文件、状态台账、dry-run 和安全连接边界；尚未替代真实 PostgreSQL 空库/历史库升级、备份恢复和发布阻断演练。迁移失败时应停止发布，保留数据库备份并按版本台账定位失败步骤，不直接手工删列或删索引。

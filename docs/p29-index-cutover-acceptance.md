# P29 索引切换、回滚与故障恢复验收

## 本地直接验证

```bash
cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2
bash scripts/acceptance/run.sh P29
```

本地安全契约确认：只有 `ready` 版本可切换；失败版本和当前活动版本不能被错误标记；状态重载、重复查询和回滚不会删除旧版本。控制平面命令示例：

```bash
go run ./cmd/index-version -state /tmp/eino-index-state.json -command switch -id shadow-v2
go run ./cmd/index-version -state /tmp/eino-index-state.json -command rollback -id stable-v1
go run ./cmd/index-version -state /tmp/eino-index-state.json -command status
```

## 真实环境必须补验

1. 切换前确认新版本完成、维度/mapping/语料快照与检索配置一致，并记录操作者、旧版和时间。
2. 用固定问题执行旧→新→旧三轮查询，确认单次请求的向量、关键词和正文版本一致。
3. 启动两个应用实例，模拟一实例延迟更新；确认未形成一半新、一半旧配置，并能看到实例确认状态。
4. 演练重建失败、切换失败和切换后回滚；旧索引在保留窗口结束前不得自动删除。

本地通过不等于真实多实例切换完成；真实 ES/Milvus 和服务发现/配置传播证据仍是 P29 的剩余验收项。

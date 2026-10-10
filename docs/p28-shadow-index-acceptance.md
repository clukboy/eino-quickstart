# P28 影子索引版本验收

## 本地直接验证

```bash
cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2
bash scripts/acceptance/run.sh P28
```

本地验收覆盖版本 ID、embedding 模型/维度、mapping、构建进度、ready/failed 门禁、持久化状态和显式切换前的旧活动版本保护。控制平面命令示例：

```bash
go run ./cmd/index-version -state /tmp/eino-index-state.json -command create \
  -id shadow-v2 -embedding embed-v2 -dimensions 1024 -mapping map-v2 -expected 100
go run ./cmd/index-version -state /tmp/eino-index-state.json -command progress \
  -id shadow-v2 -indexed 100
go run ./cmd/index-version -state /tmp/eino-index-state.json -command complete -id shadow-v2
go run ./cmd/index-version -state /tmp/eino-index-state.json -command status
```

## 真实环境必须补验

1. 为新版本创建独立 Milvus collection 和 ES index，不修改旧版本句柄。
2. 重建期间对固定问题持续查旧版，记录旧版结果和服务可用性。
3. 对新版单独执行 P20 评测，核对模型、维度、mapping、语料快照、expected/indexed 和质量报告。
4. 模拟重建失败或维度不匹配，确认旧版仍可查询且新版不能进入 ready。

当前实现是本地版本控制平面，不会假装已经写入真实 ES/Milvus；真实重建与逐版本评测必须在隔离依赖环境完成后再标记完整交付。

# P15 Rerank 预算、超时与降级验收

## 已交付边界

- `Retrieval.MaxRerankCandidates` 限制发送给重排服务的候选数量，默认安全值为 20（仅在代码直接装配且未配置时兜底）。
- `Retrieval.RerankTimeoutMS` 控制重排调用；超时不会拖垮已经完成的融合召回。
- 重排失败、超时、返回缺失/重复/越界候选时，保留原融合顺序并标记 `rerank` 降级。
- 成功时只替换前 N 个候选，预算之外的尾部候选保持融合顺序。
- `Report` 同时记录 `Enabled`、`Candidates`、`Applied`、`Err`，便于直接核对本次是否真的执行。

## 本地直接验证

```bash
cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2
bash scripts/acceptance/run.sh P15
```

报告目录会生成在 `logs/acceptance/P15/<时间>-<pid>-local/`。

## 用户核对点

1. `TestRerankIsBoundedAndReportShowsApplied`：候选数不超过预算，成功后 `Applied=true`。
2. `TestRerankFailureAndTimeoutKeepFusedResults`：服务错误或超时后，结果仍是融合召回结果，`Err` 非空且 `Degraded()` 含 `rerank`。
3. `TestRerankInvalidSubsetIsRejected`：重排服务不能凭空返回不在候选集中的文档，也不能重复返回同一文档。

## 真实环境验证

开启 P14 的真实 endpoint 后，故意让服务延迟超过 `Retrieval.RerankTimeoutMS`，确认 HTTP 搜索仍返回召回结果而不是 5xx；随后恢复服务，确认同一查询的 `rerank.Applied` 重新变为成功。真实服务的调用次数、P95 和模型质量需要在 P20/P23 的隔离报告中继续记录。

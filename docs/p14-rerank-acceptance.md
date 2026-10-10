# P14 真实 Rerank 接入与排序差异验收

## 已交付边界

P14 现在提供一个**供应商中立的 HTTP Rerank 适配器**。API 进程只在 `retrieval.enableRerank: true` 且配置了 endpoint 时启用；没有 endpoint 时配置校验会失败，不会静默打开一个不可用的重排能力。

适配器请求：

```json
{"model":"...","query":"...","documents":[{"id":"chunk-1","text":"..."}]}
```

适配器期望响应：

```json
{"results":[{"index":0,"relevance_score":0.91}]}
```

`results` 必须覆盖每一个候选下标且不能重复。适配器只返回原始候选指针的新顺序，不修改正文。

## 本地直接验证

```bash
cd /Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2
bash scripts/acceptance/run.sh P14
```

报告目录会生成在 `logs/acceptance/P14/<时间>-<pid>-local/`，重点查看 `tests.log` 和 `summary.json`。

## 接入真实服务

在 API 配置中设置：

```yaml
retrieval:
  enableRerank: true
  maxRerankCandidates: 20
  rerankEndpoint: "https://<your-rerank-service>/rerank"
  rerankApiKeyEnv: "EINO_RERANK_API_KEY"
  rerankModel: "<your-model>"
  rerankTimeoutMS: 2000
```

再设置 `EINO_RERANK_API_KEY`，启动 API。服务必须遵循上面的 JSON 契约；密钥只从环境变量读，不写入配置、不写日志。

固定同一批候选和同一个问题，分别关闭/开启 `enableRerank`，比较搜索响应中的 `data` 顺序；开启后重排服务返回的候选顺序应反映到结果中。真实服务、真实模型效果和业务阈值仍需在隔离环境单独记录，不能用本地 HTTP fixture 冒充模型质量达标。

## 异常验证

- 开启重排但不填 endpoint：配置加载失败，错误明确包含 `retrieval.rerankEndpoint`。
- 服务返回重复/越界 index：本次检索保留融合召回，不接受非法排序。
- 服务返回非 2xx：本次检索保留融合召回，并在降级列表中出现 `rerank`。

# P01：指标配置与全量测试验收

更新日期：2026-10-07（Asia/Shanghai）

## 交付结论

P01 已完成：

- API 默认 REST 配置、隔离验收配置和历史 REST 模板均显式启用 go-zero Prometheus agent。
- 指标独立监听 `127.0.0.1:9091/metrics`，不占用业务 API 的 `8090` 端口。
- 测试初始化把 Prometheus agent 改到随机临时端口，不会占用真实的 9091。
- 测试覆盖启用和禁用两种配置；禁用配置不会启动指标监听。
- 指标测试先经过真实 HTTP middleware，再通过独立 agent 端口抓取
  `http_server_requests_code_total`。
- `go test -count=1 ./...` 已通过，退出码为 0。

## 用户直接验证

在仓库根目录执行：

```bash
export REPO="/Users/clukboy/WorkBuddy/Worktrees/eino-quickstart/main-1f5054f2"
cd "$REPO"

# P01：全量 Go 测试，包含 Prometheus 启用/禁用和实际抓取测试。
bash "$REPO/scripts/acceptance/run.sh" P01

# 只查看已交付入口。
bash "$REPO/scripts/acceptance/run.sh" --list
```

如果已经按 `configs/api/restapi.yaml` 启动了 API，可以再执行真实进程级抓取：

```bash
# 不会启动或停止服务，只请求已有进程。
bash "$REPO/scripts/acceptance/run.sh" P01-live
```

自定义地址：

```bash
bash "$REPO/scripts/acceptance/run.sh" P01-live \
  http://127.0.0.1:8090 \
  http://127.0.0.1:9091/metrics
```

该命令会先请求 `/health`，再抓取独立指标地址，并要求响应包含
`http_server_requests_code_total`。服务未启动、指标端口未配置或指标未实际记录时，命令返回非 0，不能记为通过。

## 配置约定

| 配置 | 作用 |
| --- | --- |
| `Host` | 默认 `127.0.0.1`，只允许本机采集；跨主机采集必须在部署层显式改地址并配置网络限制 |
| `Port` | 默认 `9091`，与 API `8090` 分离 |
| `Path` | `/metrics` |
| API `/metrics` | 不作为业务路由提供，业务端口访问应为 404 |

禁用方式是将 `Prometheus.Host` 设为空或删除整个 `Prometheus` 配置段。go-zero 会跳过 agent 启动；本项测试通过子进程验证禁用配置不会意外监听端口。

## 证据位置

每次 `run.sh P01` 会在以下目录写入全量原始输出：

```text
logs/acceptance/P01/<run-id>/full-go-test.log
```

测试实现位于：

```text
internal/transport/restapi/chain_test.go
```

其中：

- `TestShippedConfigKeepsAgentsConfigurable` 检查发布配置完整。
- `TestPrometheusDisabledConfigDoesNotListen` 检查禁用和启用两种配置。
- `TestPrometheusAgentServesGoZeroMetrics` 检查 middleware → agent → scrape 的真实闭环。

## 边界

P01 只验证 API 进程的 go-zero HTTP 指标配置和抓取，不代表已经完成真实 PostgreSQL、Redis、Milvus、ES 或模型联调；这些属于后续批次的验收范围。

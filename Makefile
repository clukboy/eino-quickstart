# Ent enabled features | Ent 启用的官方特性
ENT_FEATURE=sql/execquery,intercept

.PHONY: gen-ent
gen-ent: # Generate Ent codes | 生成 Ent 的代码
	go run -mod=mod entgo.io/ent/cmd/ent generate --idtype uint64 --template glob="./ent/template/*.tmpl" ./ent/schema  --feature $(ENT_FEATURE)
	@echo "Generate Ent codes successfully"

# 契约入口是 docs/restapi.api。它不写类型和路由，只 import 按功能拆分的子文件：
#   docs/common/types.api        跨组共享类型
#   docs/health/health.api       健康检查
#   docs/agent/agent.api         会话
#   docs/stream/stream.api       流式对话与审批续跑（SSE）
#   docs/approver/approver.api   审批
#   docs/admin/admin.api         agent 知识库授权
#   docs/dataset/dataset.api     知识库与文档
# 改接口只动对应子文件，入口不用碰。
#
# goctl 的覆盖策略是**分两类**的（别只看下面那句 "skips existing files"）：
#
#   routes.go / types.go / restapi.json  —— 纯生成物，每次全量重写。
#       所以**不要在这三个文件里手加任何东西**：加个字段或改个 tag，下次 gen-api
#       就没了（types.go 被重写过一次，把只在 Go 里手加的 SearchHitResp.
#       ContentTruncated 冲掉了，编译才暴露）。契约字段一律写回 .api，生成物里
#       的注释也只在 .api 里维护。
#   handler/*.go、logic/*.go、svc/*.go   —— 落地即业务代码，已存在就跳过。
#       改了 .api 的签名（加 path 参数、改 returns）后它会静默跳过旧文件，于是
#       编译报「参数不匹配 / 返回值个数不符」，这时才需要 regen-api。
#   注：logic 是手写的业务实现，regen-api 不碰它。
.PHONY: gen-api
gen-api: # Generate API codes (skips existing handlers/logic, rewrites generated files) | 生成 API 代码
	goctl api go -api ./internal/transport/restapi/docs/restapi.api -dir ./internal/transport/restapi -style go_zero
	goctl api swagger -api ./internal/transport/restapi/docs/restapi.api -dir ./internal/transport/restapi
	python3 ./scripts/normalize-swagger.py ./internal/transport/restapi/restapi.json ./internal/transport/restapi/internal/types/types.go ./internal/transport/restapi/docs/swagger-overrides.json
	@echo "Generate API codes successfully"

# 强制重生成 handler 与 types：删掉 internal/handler 下所有 .go 和 types.go 再生成，
# 用于「改了 .api 签名但旧 handler 被跳过」的场合。
# 注意：它会删掉 handler 下**含手改过的**文件，以及 types.go 里一切不在 .api 里的东西；
# 不动 internal/logic 和 internal/svc，那是业务代码。
.PHONY: regen-api
regen-api: # Force-regenerate handler & types (discards edits there) | 强制重生成 handler/types
	find ./internal/transport/restapi/internal/handler -name '*.go' -delete
	rm -f ./internal/transport/restapi/internal/types/types.go
	goctl api go -api ./internal/transport/restapi/docs/restapi.api -dir ./internal/transport/restapi -style go_zero
	@echo "Regenerate API codes successfully"

# goctl api ts 会同时产出三个文件：
#   restapi.ts            路由 → 裸函数的映射（纯生成）
#   restapiComponents.ts  契约类型（纯生成）
#   gocliRequest.ts       传输层。生成器给的是裸 fetch 版本，有 4 个硬伤（GET 带
#                         body / 无鉴权 / 错误不抛 / 装不下 multipart），仓库里
#                         放的是手写的 axios 版本，**不能被覆盖**。
# 所以先生成到临时目录，只把两个纯生成文件搬过来，临时目录用完即删。
TS_DIR=./internal/transport/restapi/ts
TS_TMP=$(TS_DIR)/.goctl-tmp

.PHONY: gen-ts
gen-ts: # Generate TypeScript codes (keeps hand-written gocliRequest.ts) | 生成 TS 代码
	@rm -rf $(TS_TMP) && mkdir -p $(TS_TMP)
	goctl api ts -api ./internal/transport/restapi/docs/restapi.api -dir $(TS_TMP)
	find $(TS_TMP) -name '*.ts' ! -name 'gocliRequest.ts' -exec cp {} $(TS_DIR)/ \;
	@rm -rf $(TS_TMP)
	@echo "Generate TypeScript codes successfully (gocliRequest.ts 为手写 axios 适配层，已保留)"
.PHONY: check-api test-knowledge-lifecycle
check-api: # Read-only contract regeneration + compilation in an isolated directory
	bash ./scripts/check-api.sh

test-knowledge-lifecycle: # Deterministic lifecycle regression, no external services
	go test -count=1 ./internal/application/knowledge -run TestDocumentLifecycle
	go test -count=1 ./internal/transport/restapi/internal/handler/dataset -run TestDocumentHTTPContract
	python3 -m unittest discover -s scripts/acceptance -p 'test_*.py'

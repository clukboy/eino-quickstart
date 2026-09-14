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
#   docs/approver/approver.api   审批
#   docs/admin/admin.api         知识库管理
# 改接口只动对应子文件，入口不用碰。
.PHONY: gen-api
gen-api: # Generate API codes (skips existing files) | 生成 API 代码
	goctl api go -api ./internal/transport/restapi/docs/restapi.api -dir ./internal/transport/restapi -style go_zero
	@echo "Generate API codes successfully"

# goctl 不覆盖已存在的文件。改了 .api 的类型或路由签名后，gen-api 会静默跳过旧
# handler，于是编译报「参数不匹配 / 返回值个数不符」。需要真正重生成时用这个。
# 注意：它会删掉 internal/handler 下所有 .go（含手改过的）并重新生成；
# 不动 internal/logic 和 internal/svc，那是业务代码。
.PHONY: regen-api
regen-api: # Force-regenerate handler & types (discards edits there) | 强制重生成 handler/types
	find ./internal/transport/restapi/internal/handler -name '*.go' -delete
	rm -f ./internal/transport/restapi/internal/types/types.go
	goctl api go -api ./internal/transport/restapi/docs/restapi.api -dir ./internal/transport/restapi -style go_zero
	@echo "Regenerate API codes successfully"
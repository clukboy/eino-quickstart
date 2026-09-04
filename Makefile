# Ent enabled features | Ent 启用的官方特性
ENT_FEATURE=sql/execquery,intercept

.PHONY: gen-ent
gen-ent: # Generate Ent codes | 生成 Ent 的代码
	go run -mod=mod entgo.io/ent/cmd/ent generate --template glob="./ent/template/*.tmpl" ./ent/schema --feature $(ENT_FEATURE)
	@echo "Generate Ent codes successfully"


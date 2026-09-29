package restapi

import (
	"testing"

	"eino-quickstart/internal/transport/restapi/internal/config"
	"eino-quickstart/internal/transport/restapi/internal/handler"
	"eino-quickstart/internal/transport/restapi/internal/svc"

	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/router"
)

// TestRoutingContract 钉住 docs/*.api 生成出来的路由面：方法、完整路径，以及
// 它们能不能真的装进 go-zero 的路由树。
//
// 为什么值得单独测：.api 是源码生成器，路由冲突要到进程启动、engine 绑定路由
// 时才暴露（届时是 logx.Must，直接退出）。而文档接口的路由形状恰好是最容易
// 冲突的一类 —— 同一个 /dataset/:id 下既有静态段（documents、reindex），又有
// 二级参数（:docId）。这个测试把那次「启动才炸」提前到 go test。
//
// server.Routes() 返回的是已经拼好 prefix 的路径，所以这里的键可以直接拿去
// 喂给 router.Handle，与启动时 engine 做的事一致。
//
// 两条 SSE 路由（POST /api/v1/chat、POST /api/v1/approvals/:id/resume）现在也在
// 这张表里。它们原先手写在 stream.go、由 restapi.go 手动 AddRoute，不从
// handler.RegisterHandlers 走，因此这个测试**看不到它们** —— 漏注册只会在客户端
// 拿到 404 时才发现。收回 stream/stream.api 之后它们与其他路由同源，表面就齐了。
func TestRoutingContract(t *testing.T) {
	server, err := rest.NewServer(rest.RestConf{
		ServiceConf: service.ServiceConf{Name: "restapi-routes-test"},
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	// 注册路由只用到角色中间件，不需要任何业务依赖。
	handler.RegisterHandlers(server, svc.NewServiceContext(config.Config{}, svc.Deps{}))

	want := map[string]bool{
		"GET /health":                                        true,
		"GET /ready":                                         true,
		"POST /api/v1/sessions":                              true,
		"GET /api/v1/sessions":                               true,
		"GET /api/v1/sessions/:id/messages":                  true,
		"POST /api/v1/auth/anonymous":                        true,
		"POST /api/v1/auth/login":                            true,
		"POST /api/v1/auth/password":                         true,
		"GET /api/v1/auth/me":                                true,
		"GET /api/v1/users":                                  true,
		"POST /api/v1/users":                                 true,
		"POST /api/v1/users/sync":                            true,
		"POST /api/v1/chat":                                  true,
		"GET /api/v1/approvals/:id":                          true,
		"POST /api/v1/approvals/:id/decision":                true,
		"POST /api/v1/approvals/:id/resume":                  true,
		"POST /api/v1/dataset":                               true,
		"GET /api/v1/dataset":                                true,
		"GET /api/v1/dataset/:id":                            true,
		"DELETE /api/v1/dataset/:id":                         true,
		"POST /api/v1/dataset/:id/documents":                 true,
		"GET /api/v1/dataset/:id/documents":                  true,
		"GET /api/v1/dataset/:id/documents/:docId":           true,
		"PUT /api/v1/dataset/:id/documents/:docId":           true,
		"DELETE /api/v1/dataset/:id/documents/:docId":        true,
		"POST /api/v1/dataset/:id/documents/:docId/reindex":  true,
		"POST /api/v1/dataset/:id/documents/upload":          true,
		"PATCH /api/v1/dataset/:id/documents/:docId/enabled": true,
		"GET /api/v1/dataset/:id/documents/:docId/content":   true,
		"POST /api/v1/dataset/:id/reindex":                   true,
		"POST /api/v1/dataset/:id/search":                    true,
		"GET /api/v1/agents/:subject/dataset":                true,
		"PUT /api/v1/agents/:subject/dataset/:id":            true,
		"DELETE /api/v1/agents/:subject/dataset/:id":         true,
	}

	got := make(map[string]bool, len(want))
	routes := router.NewRouter()
	for _, route := range server.Routes() {
		key := route.Method + " " + route.Path
		if got[key] {
			t.Errorf("route %s is registered twice", key)
			continue
		}
		got[key] = true
		if err := routes.Handle(route.Method, route.Path, route.Handler); err != nil {
			t.Errorf("go-zero rejected %s: %v", key, err)
		}
	}

	for key := range want {
		if !got[key] {
			t.Errorf("route %s is missing from the generated handlers", key)
		}
	}
	for key := range got {
		if !want[key] {
			t.Errorf("route %s is not part of the declared contract", key)
		}
	}
}

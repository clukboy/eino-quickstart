package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
)

// TestPasswordGuardIsTheOnlyThingThatForcesTheChange 钉住密码守卫的三条边界。
//
// 这个中间件值得单独测，因为它的失败方式全是**静默**的：写宽一点（放行整个
// /api/v1/）等于没做，写窄一点（把 /auth/me 也拦了）会把用户锁死在「必须改密」
// 与「改密接口也进不去」之间，而两种情况的编译、启动、其他测试全都是绿的。
func TestPasswordGuardIsTheOnlyThingThatForcesTheChange(t *testing.T) {
	// httpx.Fail 走的是**进程级**错误处理器，而它由 restapi.Server.Engine()
	// 安装。这里手工装一次，否则 go-zero 的默认处理器会把 403 一律答成 400 ——
	// 那时失败信息看着像「守卫没生效」，其实只是信封没装。
	//
	// 这也说明守卫的对外形状依赖 Engine() 先调用 httpx.Register()；线上路径
	// 是「先 Register 再 RegisterHandlers」，顺序写在 restapi.go 里。
	httpx.Register()

	cases := map[string]struct {
		identity *auth.Identity
		path     string
		want     int
	}{
		"no identity passes through": {
			// 公开路由（/health、/auth/login）本来就没有 Authorization，
			// 是否允许它们不该由这里决定。
			identity: nil,
			path:     "/api/v1/sessions",
			want:     http.StatusOK,
		},
		"identity that already changed the password passes through": {
			identity: &auth.Identity{Subject: "user:1", Role: auth.RoleAgent},
			path:     "/api/v1/sessions",
			want:     http.StatusOK,
		},
		"pending password change is blocked elsewhere": {
			identity: &auth.Identity{
				Subject:            "user:1",
				Role:               auth.RoleAgent,
				MustChangePassword: true,
			},
			path: "/api/v1/sessions",
			want: http.StatusForbidden,
		},
		// 豁免前缀必须**恰好**覆盖 /auth/*：少一条，用户就被锁在改密页外面。
		"the password endpoint itself is exempt": {
			identity: &auth.Identity{
				Subject:            "user:1",
				Role:               auth.RoleAgent,
				MustChangePassword: true,
			},
			path: "/api/v1/auth/password",
			want: http.StatusOK,
		},
		"the identity endpoint is exempt": {
			// 没有它，前端无法回答「我现在是谁、要不要改密」。被拦住的话用户
			// 连该去哪都不知道。
			identity: &auth.Identity{
				Subject:            "user:1",
				Role:               auth.RoleAgent,
				MustChangePassword: true,
			},
			path: "/api/v1/auth/me",
			want: http.StatusOK,
		},
		// 边界：豁免的是 `/api/v1/auth/`（带尾斜杠），不是 `/api/v1/auth`。
		// 写成后者会连带放行 `/api/v1/authz...` 这类同前缀的路径。
		"partial prefix is not exempt": {
			identity: &auth.Identity{
				Subject:            "user:1",
				Role:               auth.RoleAgent,
				MustChangePassword: true,
			},
			path: "/api/v1/auth",
			want: http.StatusForbidden,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			handler := PasswordGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.identity != nil {
				request = request.WithContext(
					auth.WithIdentity(request.Context(), *tc.identity),
				)
			}

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}

			if tc.want != http.StatusForbidden {
				return
			}

			// 403 的文案是前端原样展示的（唯一处置动作就是去改密），
			// 所以它必须出现在响应体里，而不只是一个 code。
			var body struct {
				Code  string `json:"code"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
			}
			if body.Code != "forbidden" {
				t.Errorf("code = %q, want %q", body.Code, "forbidden")
			}
			if body.Error != passwordChangeRequiredMessage {
				t.Errorf("error = %q, want %q", body.Error, passwordChangeRequiredMessage)
			}
		})
	}
}

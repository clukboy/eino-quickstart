package middleware

import (
	"net/http"
	"strings"

	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
)

const (
	// passwordGuardExemptPrefix 是「待改密」状态下唯一还能访问的 API 前缀。
	//
	// 只有 /api/v1/auth/ 下面的接口才放行：/auth/me 要能回答「你现在是谁、还
	// 需不需要改密」（前端靠它决定显示登录表单还是改密表单），/auth/password
	// 是唯一能解开这个状态的接口。放行再宽一点（比如整个 /api/v1/）就等于没有
	// 这个守卫。
	passwordGuardExemptPrefix = "/api/v1/auth/"

	// passwordChangeRequiredMessage 是给最终用户看的文案。
	//
	// 中文而不是 code：前端会原样展示它，而这条 403 的唯一处置动作就是
	// 「去改密码」，用 code 反而要前端再维护一张文案表。
	passwordChangeRequiredMessage = "请先修改初始密码"
)

// PasswordGuard 强制「用初始密码登录后必须先改密码」。
//
// ── 为什么必须在服务端 ────────────────────────────────────────────
// 前端当然也会把用户导向改密页，但那只是体验。真正的风险是：管理员设的初始
// 密码**管理员自己知道**，如果只在前端拦，那么用 curl 带上这枚令牌就能一直
// 用那个共享密码操作下去，而界面上的「请改密」毫无约束力。
//
// ── 为什么放在全局中间件而不是逐个路由 ──────────────────────────
// 逐个路由加会漏 —— 每加一个新接口都要记得挂一次，而漏掉的那次不会有任何
// 症状（接口照常工作）。放在 server.Use 里意味着**默认拒绝**：新接口自动被
// 覆盖，只有显式列进 exempt 前缀的才放行。这个方向不能反。
//
// ── 契约 ─────────────────────────────────────────────────────────
//   - 没有身份 → 放行。公开路由（/health、/ready、/auth/login）本来就没有
//     Authorization，是否允许它们不该由这里决定；有角色要求的组由各自的
//     role 中间件拒绝。
//   - 有身份但 MustChangePassword 为假 → 放行。静态 API key 与匿名令牌恒为
//     假（前者是运维身份、无密码可改，后者不是账号），所以这两条链路完全不受
//     影响。
//   - 有身份且待改密，路径不在 exempt 前缀内 → 403。
func PasswordGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if ok && identity.MustChangePassword && !isPasswordGuardExempt(r.URL.Path) {
			httpx.Fail(r.Context(), w, httpx.Forbidden(passwordChangeRequiredMessage))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isPasswordGuardExempt(path string) bool {
	return strings.HasPrefix(path, passwordGuardExemptPrefix)
}

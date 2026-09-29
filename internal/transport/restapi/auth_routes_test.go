package restapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"eino-quickstart/internal/application/agent"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/platform/persistence/run"
	"eino-quickstart/internal/platform/persistence/session"
	"eino-quickstart/internal/platform/persistence/turn"
	"eino-quickstart/internal/transport/restapi/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// 这组测试钉住身份接口的**拒绝**契约：谁被挡、挡住时回什么码、什么文案。
//
// 它们全都不碰数据库，而这不是取巧 —— 被挡住的路径本来就该在触库之前结束。
// AccountStore 传的是零值（内部 client 为 nil），于是任何一条提前走到库里的
// 路径都会 panic 并让测试失败。这是刻意的绊线：若哪天有人把「先查库再判身份」
// 的顺序调过来，这里会立刻炸，而不是继续绿着。
const (
	testAnonymousIssuerSecret = "test-anonymous-issuer-secret"
	testAccountIssuerSecret   = "test-account-issuer-secret"
	testAdminSubject          = "platform-admin"
)

// newAuthTestEngine 起一个只带身份相关依赖的引擎。
//
// accountsEnabled 决定账号功能的两半是否装配：两半要么一起给、要么一起不给
// （restapi.New 里有这条校验，这里直接构造 Server 所以由我们保证）。关闭时必须
// 回 503 而不是「登录成功但查不到人」。
func newAuthTestEngine(t *testing.T, accountsEnabled bool) *rest.Serverless {
	t.Helper()
	ensureSetUp(t)

	anonymousIssuer, err := auth.NewAnonymousIssuer(auth.AnonymousConfig{
		Secret: testAnonymousIssuerSecret,
		TTL:    time.Hour,
		Role:   auth.RoleAgent,
	})
	if err != nil {
		t.Fatalf("build anonymous issuer: %v", err)
	}

	var (
		accountIssuer *auth.AccountIssuer
		accountStore  *account.Store
	)
	if accountsEnabled {
		accountIssuer, err = auth.NewAccountIssuer(auth.AccountConfig{
			Secret: testAccountIssuerSecret,
			TTL:    time.Hour,
		})
		if err != nil {
			t.Fatalf("build account issuer: %v", err)
		}
		accountStore = &account.Store{}
	}

	authenticator, err := auth.New(
		[]auth.APIKey{{
			Secret:   testAdminSecret,
			Identity: auth.Identity{Subject: testAdminSubject, Role: auth.RoleAdmin},
		}},
		auth.WithAnonymous(anonymousIssuer),
		auth.WithAccounts(accountIssuer),
	)
	if err != nil {
		t.Fatalf("build authenticator: %v", err)
	}

	server := &Server{
		Deps: svc.Deps{
			Agent:        &agent.Harness{},
			Sessions:     &session.Store{},
			Approvals:    &approval.Store{},
			Runs:         &run.Store{},
			Turns:        &turn.Store{},
			Auth:         authenticator,
			Anonymous:    anonymousIssuer,
			Accounts:     accountIssuer,
			AccountStore: accountStore,
			Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		},
		Config: loadShippedConfig(t),
	}

	engine, err := server.Engine()
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	serverless, err := rest.NewServerless(engine)
	if err != nil {
		t.Fatalf("serverless: %v", err)
	}

	return serverless
}

func doJSON(
	t *testing.T,
	engine *rest.Serverless,
	method string,
	path string,
	token string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	var payload io.Reader
	if body != "" {
		payload = strings.NewReader(body)
	}

	request := httptest.NewRequest(method, path, payload)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	recorder := httptest.NewRecorder()
	engine.Serve(recorder, request)

	return recorder
}

type errorEnvelope struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) errorEnvelope {
	t.Helper()

	var envelope errorEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error body %q: %v", recorder.Body.String(), err)
	}

	return envelope
}

// anonymousToken 走真实接口拿一个匿名令牌 —— 不自己签，是为了让测试用的令牌
// 与线上完全同源（同一个 issuer、同样的载荷形状）。
func anonymousToken(t *testing.T, engine *rest.Serverless) string {
	t.Helper()

	recorder := doJSON(t, engine, http.MethodPost, "/api/v1/auth/anonymous", "", "{}")
	if recorder.Code != http.StatusOK {
		t.Fatalf("issue anonymous token: status = %d; body=%s", recorder.Code, recorder.Body.String())
	}

	var resp struct {
		Token   string `json:"token"`
		Subject string `json:"subject"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode anonymous token: %v", err)
	}
	if resp.Token == "" || !strings.HasPrefix(resp.Subject, "anon:") {
		t.Fatalf("anonymous token response looks wrong: %+v", resp)
	}

	return resp.Token
}

// 账号功能关掉时，登录必须明确回 503 —— 不能「签一个没人能校验的身份」，也不能
// 落回某个默认用户。退化的后果是所有人共用同一个 subject，而症状只是「会话还是
// 互相可见」，与没做这件事一模一样。
func TestLoginIsUnavailableWhenAccountsAreDisabled(t *testing.T) {
	engine := newAuthTestEngine(t, false)

	recorder := doJSON(
		t, engine, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"alice","password":"whatever"}`,
	)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", recorder.Code, recorder.Body.String())
	}
	if envelope := decodeError(t, recorder); envelope.Code != "service_unavailable" {
		t.Errorf("code = %q, want service_unavailable", envelope.Code)
	}
}

// 空输入在触库之前就该被拒：它没有泄露任何「哪些用户名存在」的信息，
// 而原因是用户可操作的。
func TestLoginRejectsEmptyCredentialsBeforeTouchingTheStore(t *testing.T) {
	engine := newAuthTestEngine(t, true)

	for name, body := range map[string]string{
		"both empty":     `{"username":"","password":""}`,
		"username empty": `{"username":"","password":"whatever"}`,
		"password empty": `{"username":"alice","password":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := doJSON(t, engine, http.MethodPost, "/api/v1/auth/login", "", body)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
			}
			if envelope := decodeError(t, recorder); envelope.Code != "bad_request" {
				t.Errorf("code = %q, want bad_request", envelope.Code)
			}
		})
	}
}

// /auth/me 对「没有令牌」回 401：前端要靠状态码区分「根本没有身份」与
// 「身份有效但我没登录（访客）」，两者都回 200 的话这个区别就没了。
func TestGetMeRequiresAnIdentity(t *testing.T) {
	engine := newAuthTestEngine(t, true)

	recorder := doJSON(t, engine, http.MethodGet, "/api/v1/auth/me", "", "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", recorder.Code, recorder.Body.String())
	}
}

// 匿名令牌与静态 key 都必须被报成「未登录」。对用户端来说「我没登录」才是它需要
// 知道的事实，而「当前其实是一个 admin key」不该由这个接口告诉一个可能并未持有
// 它的客户端。
func TestGetMeReportsNonAccountIdentitiesAsNotLoggedIn(t *testing.T) {
	engine := newAuthTestEngine(t, true)

	cases := map[string]struct {
		token string
		want  string
	}{
		"anonymous": {token: anonymousToken(t, engine), want: "anon:"},
		"static key": {
			token: testAdminSecret,
			want:  testAdminSubject,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := doJSON(t, engine, http.MethodGet, "/api/v1/auth/me", tc.token, "")
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
			}

			var body struct {
				Authenticated bool   `json:"authenticated"`
				Subject       string `json:"subject"`
				Role          string `json:"role"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body %q: %v", recorder.Body.String(), err)
			}

			if body.Authenticated {
				t.Error("authenticated = true, want false for a non-account identity")
			}
			if !strings.HasPrefix(body.Subject, tc.want) {
				t.Errorf("subject = %q, want prefix %q", body.Subject, tc.want)
			}
			if body.Role == "" {
				t.Error("role is empty: the front end uses it to explain what this identity may do")
			}
		})
	}
}

// 改密只能对**登录账号**做。匿名令牌与静态 key 都没有密码可改，必须在触库之前
// 被拒 —— 这也是「本组不挂角色中间件」之后需要自己补的那一环。
func TestChangePasswordRejectsNonAccountIdentities(t *testing.T) {
	engine := newAuthTestEngine(t, true)

	body := `{"current_password":"old-password","new_password":"new-password"}`

	for name, token := range map[string]string{
		"no token":   "",
		"anonymous":  anonymousToken(t, engine),
		"static key": testAdminSecret,
	} {
		t.Run(name, func(t *testing.T) {
			recorder := doJSON(t, engine, http.MethodPost, "/api/v1/auth/password", token, body)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// 用户管理是管理动作：没有令牌 401，有令牌但不是 admin 403（admin 恒真，
// 见 middleware/roles.go）。
func TestUsersGroupRequiresAdminRole(t *testing.T) {
	engine := newAuthTestEngine(t, true)

	cases := map[string]struct {
		token string
		want  int
	}{
		"no token":  {"", http.StatusUnauthorized},
		"anonymous": {anonymousToken(t, engine), http.StatusForbidden},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			recorder := doJSON(t, engine, http.MethodGet, "/api/v1/users", tc.token, "")

			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tc.want, recorder.Body.String())
			}
		})
	}
}

// 同步是**占位**：它必须回 501（而不是 200 + 空结果），否则管理员会以为同步成功、
// 只是暂时没拉到人。501 与 503 的区别也要守住 —— 前者是「还没做」，后者是
// 「不提供」。
func TestUserSyncIsExplicitlyNotImplemented(t *testing.T) {
	engine := newAuthTestEngine(t, true)

	recorder := doJSON(t, engine, http.MethodPost, "/api/v1/users/sync", testAdminSecret, "")

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body=%s", recorder.Code, recorder.Body.String())
	}

	envelope := decodeError(t, recorder)
	if envelope.Code != "not_implemented" {
		t.Errorf("code = %q, want not_implemented", envelope.Code)
	}
	if envelope.Error == "" {
		t.Error("error is empty: the front end shows this text as a hint, not a failure")
	}
}

// 密码守卫必须在**全局链里真的挂上了**，而不只是存在于 middleware 包里。
// 用一枚 mcp=true 的账号令牌打 agent 组的路由：守卫排在 Authenticate 之后、
// 角色中间件之前，所以这里必须是 403 而不是 200/500。
func TestPasswordGuardIsWiredIntoTheChain(t *testing.T) {
	engine := newAuthTestEngine(t, true)

	issuer, err := auth.NewAccountIssuer(auth.AccountConfig{
		Secret: testAccountIssuerSecret,
		TTL:    time.Hour,
	})
	if err != nil {
		t.Fatalf("build account issuer: %v", err)
	}

	token, _, err := issuer.IssueFor(
		auth.NewAccountSubject(1),
		auth.RoleAgent,
		true, // 还没改过管理员设定的初始密码
	)
	if err != nil {
		t.Fatalf("issue pending-password token: %v", err)
	}

	recorder := doJSON(t, engine, http.MethodPost, "/api/v1/sessions", token, "{}")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", recorder.Code, recorder.Body.String())
	}
	if envelope := decodeError(t, recorder); envelope.Code != "forbidden" {
		t.Errorf("code = %q, want forbidden", envelope.Code)
	}
}

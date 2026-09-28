package restapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeromicro/go-zero/rest"
)

// postStream 发一个 POST，可带 JSON 请求体。
func postStream(t *testing.T, engine *rest.Serverless, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	var payload io.Reader
	if body != "" {
		payload = strings.NewReader(body)
	}

	request := httptest.NewRequest(http.MethodPost, path, payload)
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

// TestStreamRoutesAreServed 钉住两条 SSE 路由的接入面。
//
// 它们原先手写在 stream.go、由 restapi.go 手动 AddRoute：路由注册、SSE 头、帧
// 写出三件事都不在生成链路里，链路上任何测试都碰不到（routes_test.go 只扫
// handler.RegisterHandlers 出来的路由）。现在它们由 docs/stream/stream.api
// 生成，这个测试替 generation 兜住「挂上了」这件事：
//
//   - 没带 token 时必须是 401 而不是 404。404 就是路由没注册 —— 手写注册时代最
//     容易犯、也最晚发现的错（客户端拿到 404 才知道）。
//   - 401 出自全局 Authenticate，说明路由进了正常的中间件链，而不是被谁旁路掉了；
//     角色中间件（RoleAgent）在这一层之后，它有没有跟着 .api 的 middleware 走由
//     routes_test.go 的路由表 + 中间件链测试覆盖。
func TestStreamRoutesAreServed(t *testing.T) {
	engine := newTestEngine(t)

	for _, path := range []string{"/api/v1/chat", "/api/v1/approvals/unknown/resume"} {
		recorder := postStream(t, engine, path, "", `{}`)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token: status = %d, want %d (404 would mean the route is missing)",
				path, recorder.Code, http.StatusUnauthorized)
		}
	}
}

// TestChatRequestValidation 钉住请求入口的两道校验各由谁负责。
//
// 这件事值得单独测，是因为它决定了「什么时候还能拿到 HTTP 错误码」：
//
//	必填字段缺失/为空  -> 生成 handler 里的 httpx.Parse 挡掉，400 + JSON 错误信封
//	                      （go-zero 的 mapping 把没带 `optional` 的字段一律当必填，
//	                      所以 ChatReq.message 缺省不是空串，而是 400）
//	过了解析但语义非法  -> 到 logic 里，按新契约走流上的 error 帧，不再是 400
//
// 第二类是刻意的契约变更（见 docs/stream/stream.api）：生成的 SSE handler 只把
// logic 返回的 error 记日志、不写响应，所以「message 只有空白」这类判断只能由
// logic 自己发帧告诉客户端。
func TestChatRequestValidation(t *testing.T) {
	engine := newTestEngine(t)

	missing := postStream(t, engine, "/api/v1/chat", testAdminSecret, `{}`)
	if missing.Code != http.StatusBadRequest {
		t.Errorf("missing message: status = %d, want %d; body = %s",
			missing.Code, http.StatusBadRequest, missing.Body.String())
	}
	// 走 .api 生成之后，解析失败的响应头仍然是 JSON 而不是 text/event-stream：
	// userError 那条路径上 go-zero 会重设 Content-Type。客户端据此只需按状态码
	// 分流 —— 非 2xx 是 JSON 信封，2xx 才能当帧流读。
	if contentType := missing.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("missing message: Content-Type = %q, want JSON", contentType)
	}

	blank := postStream(t, engine, "/api/v1/chat", testAdminSecret, `{"message":"   "}`)
	if blank.Code != http.StatusOK {
		t.Fatalf("blank message: status = %d, want %d; body = %s",
			blank.Code, http.StatusOK, blank.Body.String())
	}

	body := blank.Body.String()
	if !strings.Contains(body, `"type":"error"`) {
		t.Errorf("blank message: body = %q, want an error frame", body)
	}
	if !strings.Contains(body, "message is empty") {
		t.Errorf("blank message: body = %q, want the empty-message reason", body)
	}
}

// TestChatFramesUseTheGeneratedFormat 走完一条真实的帧路径：请求进 handler、
// logic 推帧、生成 handler 序列化写出。
//
// 断言的帧格式是契约的一部分，必须有测试守着 —— 否则下次有人「顺手」把手写的
// 命名事件版本改回来时，不会有任何提示：
//
//   - 帧只有 `data:` 行，没有 `event:` 行（生成器的固定形态，事件种类看 payload
//     的 type 字段）
//   - 响应头是 text/event-stream，由路由上的 rest.WithSSE() 预置
//
// 真实的流式回答不在这里测：那要跑 Agent 与数据库，属于端到端范畴，见
// docs/rag-testing.md 的「对话」一节。这里用的是空 stub 依赖，请求在碰库之前
// 就被 logic 拦下了。
func TestChatFramesUseTheGeneratedFormat(t *testing.T) {
	engine := newTestEngine(t)

	recorder := postStream(t, engine, "/api/v1/chat", testAdminSecret, `{"message":"   "}`)

	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", contentType, "text/event-stream")
	}

	body := recorder.Body.String()
	if strings.Contains(body, "event:") {
		t.Errorf("frame carries an event: line, but the generated handler never writes one: %q", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("frame is not terminated by a blank line: %q", body)
	}
}

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/rest"
	resthandler "github.com/zeromicro/go-zero/rest/handler"
)

// TraceID 的价值全在「trace id 非空时才写头」这一条上：写空值比不写更糟，
// 因为它看起来像一个有效答案。这里把两种情形都钉住。
func TestTraceID(t *testing.T) {
	t.Run("with trace id", func(t *testing.T) {
		// SetUp 会安装 TracerProvider；没有它 trace.TraceIDFromContext 恒为空。
		conf := service.ServiceConf{Name: "traceid-test"}
		if err := conf.SetUp(); err != nil {
			t.Fatalf("SetUp: %v", err)
		}

		var seen string
		inner := func(w http.ResponseWriter, _ *http.Request) {
			seen = w.Header().Get(TraceIDHeader)
		}
		// 和真实链路同序：TraceHandler 起 span → 我们的中间件回写头 → handler。
		handler := resthandler.TraceHandler(conf.Name, "/x")(rest.ToMiddleware(TraceID)(inner))

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/x", nil))

		if seen == "" {
			t.Fatal("X-Trace-ID was not set on a request that has a trace id")
		}
		if got := recorder.Header().Get(TraceIDHeader); got != seen {
			t.Fatalf("handler saw %q, client saw %q", seen, got)
		}
	})

	t.Run("without trace id", func(t *testing.T) {
		var present bool
		inner := func(w http.ResponseWriter, _ *http.Request) {
			_, present = w.Header()[TraceIDHeader]
		}
		// 裸 httptest 请求没有 span，也就没有 trace id。
		rest.ToMiddleware(TraceID)(inner)(
			httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/x", nil),
		)

		if present {
			t.Fatal("X-Trace-ID was written with an empty value")
		}
	})
}

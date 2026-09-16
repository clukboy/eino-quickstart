package observability

import (
	"log/slog"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
)

// slogWriter adapts the project's slog.Logger to an io.Writer so go-zero's
// logx can write through it.
type slogWriter struct {
	logger *slog.Logger
}

func (w slogWriter) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	if line != "" {
		w.logger.Info(line, slog.String("component", "go-zero"))
	}
	return len(p), nil
}

// installLogxWriter points logx at the project logger. Any startup warning,
// access log or internal error go-zero produces then lands in the same JSON log
// file as the rest of the harness, instead of a second, separately configured
// sink.
func installLogxWriter(logger *slog.Logger) {
	logx.SetWriter(logx.NewWriter(slogWriter{logger: logger}))
}

// BridgeLogx 把 go-zero 的 logx 输出接到项目统一的 slog 上。
//
// 它属于观测层而不是 HTTP 层：worker 进程根本不跑 go-zero 的 rest 传输，
// 但 asynq 的内部日志同样走 logx，需要落到同一个日志文件里。放在传输包里
// 会逼着 worker import 整个 HTTP 传输栈。
func BridgeLogx(logger *slog.Logger) {
	if logger == nil {
		return
	}
	installLogxWriter(logger)
}

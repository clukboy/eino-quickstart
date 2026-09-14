package restapi

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

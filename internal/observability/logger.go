package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

type loggerContextKey struct{}

func NewLogger(
	level string,
	serviceName string,
	environment string,
	logFilePath string,
	maxSizeMB int,
	maxBackups int,
	maxAgeDays int,
) (*slog.Logger, error) {
	if err := os.MkdirAll(
		filepath.Dir(logFilePath),
		0755,
	); err != nil {
		return nil, err
	}

	fileLogger := &lumberjack.Logger{
		Filename:   logFilePath,
		MaxSize:    maxSizeMB,
		MaxBackups: maxBackups,
		MaxAge:     maxAgeDays,
		Compress:   true,
	}

	handler := slog.NewJSONHandler(
		io.MultiWriter(os.Stdout, fileLogger),
		&slog.HandlerOptions{
			Level: parseLogLevel(level),
		},
	)

	return slog.New(handler).With(
		"service_name", serviceName,
		"environment", environment,
	), nil
}

func WithLogger(
	ctx context.Context,
	logger *slog.Logger,
) context.Context {
	return context.WithValue(
		ctx,
		loggerContextKey{},
		logger,
	)
}

func LoggerFromContext(
	ctx context.Context,
	fallback *slog.Logger,
) *slog.Logger {
	logger, ok := ctx.Value(loggerContextKey{}).(*slog.Logger)
	if ok && logger != nil {
		return logger
	}

	return fallback
}

func parseLogLevel(value string) slog.Level {
	switch value {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

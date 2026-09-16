package asynq

import (
	"github.com/hibiken/asynq"
	"github.com/zeromicro/go-zero/core/logx"
)

type logger struct {
	logx.Logger
}

// Fatal implements [asynq.Logger].
func (l *logger) Fatal(args ...interface{}) {
	l.Logger.Error(args...)
}

// Warn implements [asynq.Logger].
func (l *logger) Warn(args ...interface{}) {
	l.Logger.Debug(args...)
}

var _ asynq.Logger = (*logger)(nil)

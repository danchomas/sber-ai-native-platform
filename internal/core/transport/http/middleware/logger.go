package middleware

import (
	"log/slog"
	"runtime"
)

type loggerHolder struct {
	log *slog.Logger
}

func WithLogger(log *slog.Logger) *loggerHolder {
	return &loggerHolder{log: log}
}

func runtimeStack(buf []byte) int {
	return runtime.Stack(buf, false)
}

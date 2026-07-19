package app

import (
	"context"
	"log/slog"
	"sort"
)

type slogWorkerLogger struct {
	logger *slog.Logger
}

func newSlogWorkerLogger(logger *slog.Logger) slogWorkerLogger {
	if logger == nil {
		logger = slog.Default()
	}
	return slogWorkerLogger{logger: logger}
}

func (logger slogWorkerLogger) Info(message string, fields map[string]any) {
	logger.log(slog.LevelInfo, message, fields)
}

func (logger slogWorkerLogger) Warn(message string, fields map[string]any) {
	logger.log(slog.LevelWarn, message, fields)
}

func (logger slogWorkerLogger) Error(message string, fields map[string]any) {
	logger.log(slog.LevelError, message, fields)
}

func (logger slogWorkerLogger) log(level slog.Level, message string, fields map[string]any) {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	attributes := make([]slog.Attr, 0, len(keys))
	for _, key := range keys {
		attributes = append(attributes, slog.Any(key, fields[key]))
	}
	logger.logger.LogAttrs(context.Background(), level, message, attributes...)
}

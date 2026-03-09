package logger

import (
	"log/slog"
	"os"
)

// Interface defines the logging methods.
type Interface interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Fatal(msg string, args ...any)
	With(args ...any) Interface
}

type Logger struct {
	*slog.Logger
}

// NewLogger creates a new structured logger.
func NewLogger(level string) Interface {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "info":
		slogLevel = slog.LevelInfo
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogLevel,
	})

	return &Logger{
		Logger: slog.New(handler),
	}
}

func (l *Logger) With(args ...any) Interface {
	return &Logger{
		Logger: l.Logger.With(args...),
	}
}

func (l *Logger) Fatal(msg string, args ...any) {
	l.Logger.Error(msg, args...)
	os.Exit(1)
}

// No-op logger for testing
type NoOpLogger struct{}

func (n *NoOpLogger) Debug(msg string, args ...any) {}
func (n *NoOpLogger) Info(msg string, args ...any)  {}
func (n *NoOpLogger) Warn(msg string, args ...any)  {}
func (n *NoOpLogger) Error(msg string, args ...any) {}
func (n *NoOpLogger) Fatal(msg string, args ...any) { os.Exit(1) }
func (n *NoOpLogger) With(args ...any) Interface   { return n }
func NewNoOp() Interface                           { return &NoOpLogger{} }

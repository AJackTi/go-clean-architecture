// Package logger provides the application's small logging seam.
package logger

import (
	"strings"
	"sync/atomic"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var singleton atomic.Pointer[zap.Logger]

func init() {
	singleton.Store(zap.NewNop())
}

// Init builds and installs a logger for the process. Invalid levels are
// returned to the composition root instead of silently producing a logger at
// an unexpected severity.
func Init(logLevel string) error {
	level, err := zapcore.ParseLevel(strings.ToLower(strings.TrimSpace(logLevel)))
	if err != nil {
		return err
	}

	config := zap.Config{
		Encoding:    "console",
		Level:       zap.NewAtomicLevelAt(level),
		OutputPaths: []string{"stderr"},
		ErrorOutputPaths: []string{
			"stderr",
		},
		EncoderConfig: zapcore.EncoderConfig{
			MessageKey:    "message",
			StacktraceKey: "stacktrace",
			TimeKey:       "time",
			LevelKey:      "level",
			CallerKey:     "caller",
			FunctionKey:   zapcore.OmitKey,
			EncodeLevel:   zapcore.CapitalLevelEncoder,
			EncodeCaller:  zapcore.FullCallerEncoder,
			EncodeTime:    zapcore.RFC3339TimeEncoder,
		},
	}
	built, err := config.Build()
	if err != nil {
		return err
	}
	previous := singleton.Swap(built)
	if previous != nil {
		_ = previous.Sync()
	}
	return nil
}

// Sync flushes buffered log entries.
func Sync() error { return current().Sync() }

func current() *zap.Logger {
	logger := singleton.Load()
	if logger == nil {
		return zap.NewNop()
	}
	return logger
}

// Debug logs a debug message with fields.
func Debug(message string, fields ...zap.Field) { current().Debug(message, fields...) }

// Info logs an informational message with fields.
func Info(message string, fields ...zap.Field) { current().Info(message, fields...) }

// Warn logs a warning with fields.
func Warn(message string, fields ...zap.Field) { current().Warn(message, fields...) }

// Error logs an error with fields.
func Error(message string, fields ...zap.Field) { current().Error(message, fields...) }

// Fatal logs a message and exits through zap's fatal hook.
func Fatal(message string, fields ...zap.Field) { current().Fatal(message, fields...) }

// ErrWrap turns an error into a structured zap field.
func ErrWrap(err error) zap.Field { return zap.Error(err) }

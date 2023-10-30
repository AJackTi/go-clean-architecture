package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var singleton zap.Logger

// Init initializes a thread-safe singleton logger
// This would be called from a main method when the application starts up
// This function would ideally, take zap configuration, but is left out
// in favor of simplicity using the example logger.
func Init(logLevel string) {
	// once ensures the singleton is initialized only once
	zaplv, _ := zapcore.ParseLevel(logLevel)

	zapcfg := zap.Config{
		Encoding:    "console",
		Level:       zap.NewAtomicLevelAt(zaplv),
		OutputPaths: []string{"stderr"},

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

	logger, _ := zapcfg.Build()
	singleton = *logger
}

// Debug logs a debug message with the given fields
func Debug(message string, fields ...zap.Field) {
	singleton.Debug(message, fields...)
}

// Info logs a debug message with the given fields
func Info(message string, fields ...zap.Field) {
	singleton.Info(message, fields...)
}

// Warn logs a debug message with the given fields
func Warn(message string, fields ...zap.Field) {
	singleton.Warn(message, fields...)
}

// Error logs a debug message with the given fields
func Error(message string, fields ...zap.Field) {
	singleton.Error(message, fields...)
}

// Fatal logs a message
func Fatal(message string, fields ...zap.Field) {
	singleton.Fatal(message, fields...)
}

func ErrWrap(err error) zap.Field {
	return zap.Error(err)
}

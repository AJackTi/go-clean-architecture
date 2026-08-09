package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/AJackTi/go-clean-architecture/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	requestIDHeader   = "X-Request-ID"
	maxRequestIDBytes = 128
	requestLogMessage = "http request completed"
	defaultRouteName  = "unmatched"
)

// accessLogger is the small logging surface needed by the HTTP middleware.
// Keeping it local makes the middleware straightforward to test without
// changing the process-wide logger API.
type accessLogger interface {
	Info(string, ...zap.Field)
	Warn(string, ...zap.Field)
	Error(string, ...zap.Field)
}

// processAccessLogger adapts the application's logger seam to access logs.
// The logger package starts with a no-op logger, so a router can be exercised
// safely before the application composition root initializes logging.
type processAccessLogger struct{}

func (processAccessLogger) Info(message string, fields ...zap.Field) {
	logger.Info(message, fields...)
}

func (processAccessLogger) Warn(message string, fields ...zap.Field) {
	logger.Warn(message, fields...)
}

func (processAccessLogger) Error(message string, fields ...zap.Field) {
	logger.Error(message, fields...)
}

// observabilityMiddleware assigns a bounded correlation ID and emits one
// structured access event after every request. It deliberately logs the
// matched route rather than the raw URL so query strings and unbounded user
// input do not end up in process logs.
func observabilityMiddleware(accessLog accessLogger) gin.HandlerFunc {
	if accessLog == nil {
		accessLog = processAccessLogger{}
	}

	return func(c *gin.Context) {
		started := time.Now()
		requestID := requestIDFor(c)
		c.Header(requestIDHeader, requestID)

		c.Next()

		status := c.Writer.Status()
		if status <= 0 {
			status = http.StatusOK
		}
		responseBytes := c.Writer.Size()
		if responseBytes < 0 {
			responseBytes = 0
		}
		route := c.FullPath()
		if route == "" {
			route = defaultRouteName
		}
		method := ""
		if c.Request != nil {
			method = c.Request.Method
		}
		fields := []zap.Field{
			zap.String("request_id", requestID),
			zap.String("method", method),
			zap.String("route", route),
			zap.Int("status", status),
			zap.Int("bytes", responseBytes),
			zap.Duration("duration", time.Since(started)),
		}

		switch {
		case status >= http.StatusInternalServerError:
			accessLog.Error(requestLogMessage, fields...)
		case status >= http.StatusBadRequest:
			accessLog.Warn(requestLogMessage, fields...)
		default:
			accessLog.Info(requestLogMessage, fields...)
		}
	}
}

func requestIDFor(c *gin.Context) string {
	if c != nil && c.Request != nil {
		values := c.Request.Header.Values(requestIDHeader)
		if len(values) == 1 && validRequestID(values[0]) {
			return values[0]
		}
	}
	return uuid.NewString()
}

// validRequestID accepts only an HTTP token with a conservative size limit.
// This permits IDs from an upstream proxy while preventing control characters,
// whitespace, and oversized values from reaching response headers or logs.
func validRequestID(value string) bool {
	if len(value) == 0 || len(value) > maxRequestIDBytes {
		return false
	}
	for i := 0; i < len(value); i++ {
		character := value[i]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		if !strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character)) {
			return false
		}
	}
	return true
}

package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestObservabilityAssignsAndLogsGeneratedRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.DebugLevel)
	router := newRouter(nil, func(context.Context) error { return nil }, zap.New(core))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, livenessPath, nil)
	router.ServeHTTP(response, request)

	requestID := response.Header().Get(requestIDHeader)
	assertUUIDRequestID(t, requestID)
	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("access log entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Level != zapcore.InfoLevel {
		t.Fatalf("access log level = %s, want info", entry.Level)
	}
	if entry.Message != requestLogMessage {
		t.Fatalf("access log message = %q, want %q", entry.Message, requestLogMessage)
	}
	fields := entry.ContextMap()
	if fields["request_id"] != requestID {
		t.Errorf("logged request_id = %v, want %q", fields["request_id"], requestID)
	}
	if fields["method"] != http.MethodGet || fields["route"] != livenessPath || fields["status"] != int64(200) {
		t.Errorf("unexpected access fields: %#v", fields)
	}
	if bytes, ok := fields["bytes"].(int64); !ok || bytes <= 0 {
		t.Errorf("logged bytes = %#v, want positive int", fields["bytes"])
	}
	if duration, ok := fields["duration"].(time.Duration); !ok || duration < 0 {
		t.Errorf("logged duration = %#v, want non-negative duration", fields["duration"])
	}
}

func TestObservabilityPreservesAValidRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const wantRequestID = "upstream-ABC_123.~"
	core, logs := observer.New(zap.DebugLevel)
	router := newRouter(nil, nil, zap.New(core))

	request := httptest.NewRequest(http.MethodGet, livenessPath, nil)
	request.Header.Set(requestIDHeader, wantRequestID)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if got := response.Header().Get(requestIDHeader); got != wantRequestID {
		t.Fatalf("response request ID = %q, want %q", got, wantRequestID)
	}
	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("access log entries = %d, want 1", len(entries))
	}
	if got := entries[0].ContextMap()["request_id"]; got != wantRequestID {
		t.Errorf("logged request ID = %v, want %q", got, wantRequestID)
	}
}

func TestObservabilityReplacesInvalidRequestIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		values []string
	}{
		{name: "empty", values: []string{""}},
		{name: "space", values: []string{"contains space"}},
		{name: "control character", values: []string{"bad\nrequest"}},
		{name: "too long", values: []string{strings.Repeat("a", maxRequestIDBytes+1)}},
		{name: "multiple values", values: []string{"first", "second"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			router := newRouter(nil, nil, zap.New(core))
			request := httptest.NewRequest(http.MethodGet, livenessPath, nil)
			for _, value := range test.values {
				request.Header.Add(requestIDHeader, value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			got := response.Header().Get(requestIDHeader)
			assertUUIDRequestID(t, got)
			if len(logs.All()) != 1 {
				t.Fatalf("access log entries = %d, want 1", len(logs.All()))
			}
			if logged := logs.All()[0].ContextMap()["request_id"]; logged != got {
				t.Errorf("logged request ID = %v, want %q", logged, got)
			}
		})
	}
}

func TestObservabilityAddsRequestIDToNotFoundResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.DebugLevel)
	router := newRouter(nil, nil, zap.New(core))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	assertUUIDRequestID(t, response.Header().Get(requestIDHeader))
	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("access log entries = %d, want 1", len(entries))
	}
	if entries[0].Level != zapcore.WarnLevel {
		t.Fatalf("access log level = %s, want warn", entries[0].Level)
	}
	fields := entries[0].ContextMap()
	if fields["route"] != defaultRouteName || fields["status"] != int64(http.StatusNotFound) {
		t.Errorf("unexpected 404 fields: %#v", fields)
	}
}

func TestObservabilityCoversTrailingSlashNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.DebugLevel)
	router := newRouter(nil, nil, zap.New(core))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, livenessPath+"/", nil)
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	assertUUIDRequestID(t, response.Header().Get(requestIDHeader))
	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("access log entries = %d, want 1", len(entries))
	}
	if entries[0].Level != zapcore.WarnLevel {
		t.Fatalf("access log level = %s, want warn", entries[0].Level)
	}
	fields := entries[0].ContextMap()
	if fields["route"] != defaultRouteName || fields["status"] != int64(http.StatusNotFound) {
		t.Errorf("unexpected trailing-slash fields: %#v", fields)
	}
}

func TestObservabilityAndRecoveryKeepRequestIDWhenHandlerPanics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousErrorWriter := gin.DefaultErrorWriter
	var recoveryOutput bytes.Buffer
	gin.DefaultErrorWriter = &recoveryOutput
	t.Cleanup(func() { gin.DefaultErrorWriter = previousErrorWriter })

	core, logs := observer.New(zap.DebugLevel)
	router := newRouter(nil, nil, zap.New(core))
	router.GET("/panic", func(*gin.Context) { panic("panic-secret") })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/panic?token=query-secret", nil)
	request.Header.Set("Cookie", "session=cookie-secret")
	request.Header.Set("X-API-Key", "header-secret")
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if got, want := response.Body.String(), `{"error":{"code":"internal_error","message":"internal server error"}}`; got != want {
		t.Fatalf("panic body = %q, want %q", got, want)
	}
	for _, secret := range []string{"panic-secret", "query-secret", "cookie-secret", "header-secret"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Errorf("panic response contains secret %q", secret)
		}
	}
	if recoveryOutput.Len() != 0 {
		t.Fatalf("recovery wrote request/panic details to error writer: %q", recoveryOutput.String())
	}
	assertUUIDRequestID(t, response.Header().Get(requestIDHeader))
	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("access log entries = %d, want 1", len(entries))
	}
	if entries[0].Level != zapcore.ErrorLevel {
		t.Fatalf("access log level = %s, want error", entries[0].Level)
	}
	if fields := entries[0].ContextMap(); fields["status"] != int64(http.StatusInternalServerError) {
		t.Errorf("panic log fields = %#v, want status 500", fields)
	}
}

func assertUUIDRequestID(t *testing.T, value string) {
	t.Helper()
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || parsed.String() != value || parsed.Version() != uuid.Version(4) {
		t.Fatalf("request ID = %q, want canonical non-nil UUID", value)
	}
}

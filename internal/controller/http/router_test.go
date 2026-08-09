package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHealthEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		readiness  ReadinessCheck
		path       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "liveness does not depend on database",
			readiness:  func(context.Context) error { return errors.New("database offline") },
			path:       livenessPath,
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok"}`,
		},
		{
			name:       "ready",
			readiness:  func(context.Context) error { return nil },
			path:       readinessPath,
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok"}`,
		},
		{
			name:       "dependency unavailable",
			readiness:  func(context.Context) error { return errors.New("database offline") },
			path:       readinessPath,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"unavailable"}`,
		},
		{
			name:       "missing readiness check fails closed",
			readiness:  nil,
			path:       readinessPath,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"unavailable"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			NewRouter(nil, test.readiness).ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if got := recorder.Body.String(); got != test.wantBody {
				t.Fatalf("body = %q, want %q", got, test.wantBody)
			}
		})
	}
}

func TestItemRoutesAreMounted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items", nil)
	NewRouter(nil, nil).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (route should be mounted)", recorder.Code, http.StatusBadRequest)
	}
}

package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appmetrics "github.com/AJackTi/go-clean-architecture/pkg/metrics"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRouterMetricsAreOptInAndUseBoundedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metricSet := appmetrics.New()
	router := newRouter(
		nil,
		func(context.Context) error { return nil },
		zap.NewNop(),
		WithMetrics(metricSet),
	)

	serveTelemetryRequest(t, router, http.MethodGet, livenessPath+"?token=secret", nil, http.StatusOK)
	serveTelemetryRequest(t, router, http.MethodGet, "/missing/123?token=secret", nil, http.StatusNotFound)

	metricsResponse := httptest.NewRecorder()
	router.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, metricsPath, nil))
	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", metricsResponse.Code, http.StatusOK)
	}
	body := metricsResponse.Body.String()
	for _, want := range []string{
		`http_server_requests_total{method="GET",route="/api/health",status_class="2xx",status_code="200"} 1`,
		`http_server_requests_total{method="GET",route="unmatched",status_class="4xx",status_code="404"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics exposition missing %q:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{"token=secret", "/missing/123"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("metrics exposition contains raw request data %q", forbidden)
		}
	}

	disabled := NewRouter(nil, nil)
	disabledResponse := httptest.NewRecorder()
	disabled.ServeHTTP(disabledResponse, httptest.NewRequest(http.MethodGet, metricsPath, nil))
	if disabledResponse.Code != http.StatusNotFound {
		t.Fatalf("disabled metrics status = %d, want %d", disabledResponse.Code, http.StatusNotFound)
	}
}

func TestTracingExtractsParentAndCorrelatesAccessEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	core, logs := observer.New(zap.DebugLevel)
	router := newRouter(
		nil,
		func(context.Context) error { return nil },
		zap.New(core),
		WithTracing(provider.Tracer(InstrumentationName), propagation.TraceContext{}),
	)

	const (
		traceIDText  = "0af7651916cd43dd8448eb211c80319c"
		parentIDText = "b7ad6b7169203331"
		requestID    = "upstream-request-123"
	)
	headers := make(http.Header)
	headers.Set("Traceparent", "00-"+traceIDText+"-"+parentIDText+"-01")
	headers.Set(requestIDHeader, requestID)
	serveTelemetryRequest(t, router, http.MethodGet, livenessPath+"?token=secret", headers, http.StatusOK)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("exported spans = %d, want 1", len(spans))
	}
	span := spans[0]
	if span.Name != "GET "+livenessPath {
		t.Errorf("span name = %q, want bounded route name", span.Name)
	}
	wantTraceID, err := trace.TraceIDFromHex(traceIDText)
	if err != nil {
		t.Fatal(err)
	}
	wantParentID, err := trace.SpanIDFromHex(parentIDText)
	if err != nil {
		t.Fatal(err)
	}
	if span.SpanContext.TraceID() != wantTraceID || span.Parent.SpanID() != wantParentID {
		t.Errorf("span context = trace %s parent %s, want trace %s parent %s", span.SpanContext.TraceID(), span.Parent.SpanID(), wantTraceID, wantParentID)
	}
	attributes := spanAttributes(span.Attributes)
	for key, want := range map[string]any{
		"http.request.method":       http.MethodGet,
		"http.route":                livenessPath,
		"http.response.status_code": int64(http.StatusOK),
		"request.id":                requestID,
	} {
		if got := attributes[key]; got != want {
			t.Errorf("span attribute %s = %#v, want %#v", key, got, want)
		}
	}
	for _, value := range attributes {
		if text, ok := value.(string); ok && strings.Contains(text, "token=secret") {
			t.Errorf("span attributes contain query data: %#v", attributes)
		}
	}

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("access log entries = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["trace_id"] != traceIDText || fields["request_id"] != requestID {
		t.Errorf("access correlation fields = %#v", fields)
	}
	if fields["span_id"] != span.SpanContext.SpanID().String() {
		t.Errorf("logged span_id = %#v, want %s", fields["span_id"], span.SpanContext.SpanID())
	}
}

func serveTelemetryRequest(
	t *testing.T,
	router http.Handler,
	method string,
	target string,
	headers http.Header,
	wantStatus int,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	request.Header = headers.Clone()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d", method, target, response.Code, wantStatus)
	}
	return response
}

func spanAttributes(values []attribute.KeyValue) map[string]any {
	result := make(map[string]any, len(values))
	for _, value := range values {
		switch value.Value.Type() {
		case attribute.STRING:
			result[string(value.Key)] = value.Value.AsString()
		case attribute.INT64:
			result[string(value.Key)] = value.Value.AsInt64()
		}
	}
	return result
}

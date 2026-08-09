package telemetry

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

var nilContext context.Context

func TestNewWithoutEndpointDoesNotCreateExporterOrNetworkActivity(t *testing.T) {
	globalProvider := otel.GetTracerProvider()
	var factoryCalls atomic.Int32
	factory := func(context.Context, exporterSettings) (sdktrace.SpanExporter, error) {
		factoryCalls.Add(1)
		return nil, errors.New("exporter factory must not be called")
	}

	provider, err := newProvider(context.Background(), Config{
		ServiceName: "orders-api",
		Environment: "test",
	}, factory)
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}
	if provider.ExporterEnabled() {
		t.Fatal("ExporterEnabled() = true for an empty endpoint")
	}
	if got := factoryCalls.Load(); got != 0 {
		t.Fatalf("exporter factory called %d times, want 0", got)
	}
	if got := otel.GetTracerProvider(); got != globalProvider {
		t.Fatal("newProvider() mutated the global OpenTelemetry tracer provider")
	}

	_, span := provider.Tracer("telemetry.test").Start(context.Background(), "local-span")
	span.End()
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
}

func TestRealExporterDoesNotContactCollectorUntilSpanExport(t *testing.T) {
	var requests atomic.Int32
	collector := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(collector.Close)

	provider, err := New(context.Background(), Config{
		Endpoint:    collector.URL,
		Insecure:    true,
		ServiceName: "orders-api",
		SampleRatio: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("collector requests during construction = %d, want 0", got)
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("collector requests without spans = %d, want 0", got)
	}
}

func TestRealExporterUsesNormalizedOTLPHTTPPath(t *testing.T) {
	type requestRecord struct {
		method      string
		path        string
		contentType string
	}
	requests := make(chan requestRecord, 1)
	collector := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		record := requestRecord{
			method:      request.Method,
			path:        request.URL.Path,
			contentType: request.Header.Get("Content-Type"),
		}
		select {
		case requests <- record:
		default:
		}
		response.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)

	provider, err := New(context.Background(), Config{
		Endpoint:    collector.URL,
		Insecure:    true,
		ServiceName: "orders-api",
		SampleRatio: 1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	_, span := provider.Tracer("telemetry.test").Start(context.Background(), "export-me")
	span.End()
	flushContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := provider.ForceFlush(flushContext); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}

	select {
	case got := <-requests:
		if got.method != http.MethodPost || got.path != defaultTracePath {
			t.Errorf("collector request = %s %s, want POST %s", got.method, got.path, defaultTracePath)
		}
		if got.contentType != "application/x-protobuf" {
			t.Errorf("collector content type = %q, want application/x-protobuf", got.contentType)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("collector received no span export")
	}
}

func TestConfiguredProviderUsesExporterAndResourceAttributes(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	var gotSettings exporterSettings
	provider, err := newProvider(context.Background(), Config{
		Endpoint:    "http://collector.example:4318/custom/traces",
		Insecure:    true,
		ServiceName: "orders-api",
		Environment: "staging",
		SampleRatio: 1,
	}, func(_ context.Context, settings exporterSettings) (sdktrace.SpanExporter, error) {
		gotSettings = settings
		return exporter, nil
	})
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}
	if !provider.ExporterEnabled() {
		t.Fatal("ExporterEnabled() = false for a configured endpoint")
	}
	if gotSettings.endpointURL != "http://collector.example:4318/custom/traces" {
		t.Fatalf("normalized endpoint = %q", gotSettings.endpointURL)
	}

	_, span := provider.Tracer("telemetry.test").Start(context.Background(), "exported-span")
	span.End()
	if err := provider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "exported-span" {
		t.Fatalf("exported spans = %#v, want one exported span", spans)
	}
	if got := resourceAttribute(spans[0].Resource, "service.name"); got != "orders-api" {
		t.Errorf("service.name = %q, want orders-api", got)
	}
	if got := resourceAttribute(spans[0].Resource, "deployment.environment.name"); got != "staging" {
		t.Errorf("deployment.environment.name = %q, want staging", got)
	}
	if got := spans[0].InstrumentationScope.Name; got != "telemetry.test" {
		t.Errorf("instrumentation scope = %q, want telemetry.test", got)
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestZeroSampleRatioDoesNotSampleRootSpans(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider, err := newProvider(context.Background(), Config{
		Endpoint:    "http://collector.example:4318",
		Insecure:    true,
		ServiceName: "orders-api",
		SampleRatio: 0,
	}, func(context.Context, exporterSettings) (sdktrace.SpanExporter, error) {
		return exporter, nil
	})
	if err != nil {
		t.Fatalf("newProvider() error = %v", err)
	}

	_, span := provider.Tracer("telemetry.test").Start(context.Background(), "unsampled-root")
	if span.SpanContext().IsSampled() {
		t.Fatal("root span is sampled with a zero sample ratio")
	}
	span.End()
	if err := provider.ForceFlush(context.Background()); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}
	if spans := exporter.GetSpans(); len(spans) != 0 {
		t.Fatalf("exported spans = %#v, want none", spans)
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestNewPropagatesExporterConstructionFailure(t *testing.T) {
	want := errors.New("collector setup failed")
	_, err := newProvider(context.Background(), Config{
		Endpoint: "collector.example:4318",
	}, func(context.Context, exporterSettings) (sdktrace.SpanExporter, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("newProvider() error = %v, want wrapped exporter error", err)
	}

	_, err = newProvider(context.Background(), Config{
		Endpoint: "collector.example:4318",
	}, func(context.Context, exporterSettings) (sdktrace.SpanExporter, error) {
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "nil exporter") {
		t.Fatalf("newProvider() nil-exporter error = %v", err)
	}
}

func TestNormalizeEndpointAndInsecurePolicy(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		insecure bool
		want     string
		wantErr  string
	}{
		{name: "host secure default", endpoint: "collector.example:4318", want: "https://collector.example:4318/v1/traces"},
		{name: "host insecure", endpoint: "collector.example:4318", insecure: true, want: "http://collector.example:4318/v1/traces"},
		{name: "https URL", endpoint: "https://collector.example:4318/v1/traces", want: "https://collector.example:4318/v1/traces"},
		{name: "http URL", endpoint: "http://collector.example:4318/v1/traces", insecure: true, want: "http://collector.example:4318/v1/traces"},
		{name: "pathless URL gets OTLP path", endpoint: "https://collector.example:4318", want: "https://collector.example:4318/v1/traces"},
		{name: "empty endpoint", want: ""},
		{name: "http without insecure", endpoint: "http://collector.example:4318", wantErr: "scheme and insecure"},
		{name: "https with insecure", endpoint: "https://collector.example:4318", insecure: true, wantErr: "scheme and insecure"},
		{name: "unsupported scheme", endpoint: "grpc://collector.example:4317", wantErr: "scheme must be http or https"},
		{name: "missing host", endpoint: ":4318", wantErr: "invalid OTLP endpoint"},
		{name: "query rejected", endpoint: "https://collector.example:4318/v1/traces?token=secret", wantErr: "invalid OTLP endpoint"},
		{name: "empty query rejected", endpoint: "https://collector.example:4318?", wantErr: "invalid OTLP endpoint"},
		{name: "unicode whitespace rejected", endpoint: "collector.example:\u00a04318", wantErr: "must not contain whitespace"},
		{name: "insecure without endpoint", insecure: true, want: ""},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeEndpoint(test.endpoint, test.insecure)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("normalizeEndpoint() error = %v", err)
				}
				if got != test.want {
					t.Errorf("normalizeEndpoint() = %q, want %q", got, test.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("normalizeEndpoint() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestConfigValidationRejectsInvalidValues(t *testing.T) {
	tests := []Config{
		{SampleRatio: -0.01},
		{SampleRatio: 1.01},
		{SampleRatio: math.NaN()},
		{SampleRatio: math.Inf(1)},
		{ServiceName: "orders\napi"},
		{ServiceName: strings.Repeat("a", maxServiceNameBytes+1)},
		{Environment: "staging\x00"},
		{Environment: strings.Repeat("a", maxEnvironmentBytes+1)},
		{Endpoint: "https://" + strings.Repeat("a", maxEndpointBytes)},
	}
	for _, cfg := range tests {
		if err := cfg.Validate(); err == nil {
			t.Errorf("Config.Validate() unexpectedly accepted %#v", cfg)
		}
	}

	err := (Config{Endpoint: "https://collector.example/v1/traces?token=super-secret"}).Validate()
	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("endpoint validation error must be sanitized, got %v", err)
	}
}

func TestLifecycleRejectsNilContexts(t *testing.T) {
	if _, err := New(nilContext, Config{}); !errors.Is(err, ErrNilContext) {
		t.Fatalf("New(nil, ...) error = %v, want ErrNilContext", err)
	}
	provider, err := New(context.Background(), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.ForceFlush(nilContext); !errors.Is(err, ErrNilContext) {
		t.Fatalf("ForceFlush(nil) error = %v, want ErrNilContext", err)
	}
	if err := provider.Shutdown(nilContext); !errors.Is(err, ErrNilContext) {
		t.Fatalf("Shutdown(nil) error = %v, want ErrNilContext", err)
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func resourceAttribute(res *resource.Resource, key string) string {
	if res == nil {
		return ""
	}
	for _, value := range res.Attributes() {
		if string(value.Key) == key {
			return value.Value.AsString()
		}
	}
	return ""
}

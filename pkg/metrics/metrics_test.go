package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

func TestNewCreatesIndependentRegistries(t *testing.T) {
	first := New()
	second := New()
	if first.Registry() == second.Registry() {
		t.Fatal("New returned a shared registry")
	}

	first.Observe(http.MethodGet, "/api/health", http.StatusOK, time.Millisecond)
	if got := testutil.ToFloat64(first.requests.WithLabelValues("GET", "/api/health", "2xx", "200")); got != 1 {
		t.Fatalf("first request count = %v, want 1", got)
	}
	if families, err := second.Registry().Gather(); err != nil {
		t.Fatalf("gather second registry: %v", err)
	} else if len(families) != 0 {
		t.Fatalf("second registry has %d metric families, want 0", len(families))
	}
}

func TestObserveRecordsCounterAndHistogramLabels(t *testing.T) {
	metrics := New()
	metrics.Observe(" get ", "/api/v1/items/:id", http.StatusCreated, 150*time.Millisecond)

	counter := metrics.requests.WithLabelValues("GET", "/api/v1/items/:id", "2xx", "201")
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("request count = %v, want 1", got)
	}

	family := metricFamily(t, metrics, durationMetricName)
	if got := len(family.Metric); got != 1 {
		t.Fatalf("duration series = %d, want 1", got)
	}
	histogram := family.Metric[0].GetHistogram()
	if histogram == nil {
		t.Fatal("duration metric has no histogram")
	}
	if histogram.GetSampleCount() != 1 {
		t.Fatalf("duration sample count = %d, want 1", histogram.GetSampleCount())
	}
	if got := histogram.GetSampleSum(); got < 0.149 || got > 0.151 {
		t.Fatalf("duration sample sum = %v, want approximately 0.15", got)
	}
	labels := labelsOf(family.Metric[0])
	for key, want := range map[string]string{
		"method":       "GET",
		"route":        "/api/v1/items/:id",
		"status_class": "2xx",
		"status_code":  "201",
	} {
		if labels[key] != want {
			t.Errorf("duration label %s = %q, want %q", key, labels[key], want)
		}
	}
}

func TestObserveNormalizesInvalidMethodStatusAndDuration(t *testing.T) {
	metrics := New()
	metrics.Observe(" custom-method ", "/api/health", 700, -time.Second)

	if got := testutil.ToFloat64(metrics.requests.WithLabelValues("OTHER", "/api/health", "unknown", "unknown")); got != 1 {
		t.Fatalf("normalized request count = %v, want 1", got)
	}
	family := metricFamily(t, metrics, durationMetricName)
	if got := family.Metric[0].GetHistogram().GetSampleSum(); got != 0 {
		t.Fatalf("negative duration was not clamped: sum = %v", got)
	}
}

func TestNormalizeRouteCollapsesUnknownAndRawPaths(t *testing.T) {
	const uuidPath = "/api/v1/items/c80c1043-a6cd-42bf-984e-0191352f4b26"
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: UnknownRoute},
		{name: "already unmatched", input: UnknownRoute, want: UnknownRoute},
		{name: "numeric raw path", input: "/api/v1/items/123", want: UnknownRoute},
		{name: "uuid raw path", input: uuidPath, want: UnknownRoute},
		{name: "query string", input: "/api/v1/items?limit=20", want: UnknownRoute},
		{name: "absolute URL", input: "https://example.test/api/v1/items", want: UnknownRoute},
		{name: "dot segment", input: "/api/./health", want: UnknownRoute},
		{name: "parent segment", input: "/api/../health", want: UnknownRoute},
		{name: "oversized", input: "/" + strings.Repeat("x", maxRouteBytes), want: UnknownRoute},
		{name: "gin placeholder", input: "/api/v1/items/:id", want: "/api/v1/items/:id"},
		{name: "openapi placeholder", input: "/api/v1/items/{id}", want: "/api/v1/items/:id"},
		{name: "trailing slash", input: "/api/health/", want: "/api/health"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeRoute(test.input); got != test.want {
				t.Errorf("NormalizeRoute(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestObserveBoundsRawRouteCardinality(t *testing.T) {
	metrics := New()
	paths := []string{
		"/api/v1/items/1",
		"/api/v1/items/2",
		"/api/v1/items/c80c1043-a6cd-42bf-984e-0191352f4b26",
		"/api/v1/items?name=secret",
	}
	for _, path := range paths {
		metrics.Observe(http.MethodGet, path, http.StatusNotFound, time.Millisecond)
	}

	family := metricFamily(t, metrics, requestsMetricName)
	routes := make(map[string]struct{})
	for _, metric := range family.Metric {
		routes[labelsOf(metric)["route"]] = struct{}{}
	}
	if len(routes) != 1 {
		t.Fatalf("raw paths produced %d route labels (%v), want 1", len(routes), routes)
	}
	if _, exists := routes[UnknownRoute]; !exists {
		t.Fatalf("raw paths route labels = %v, want %q", routes, UnknownRoute)
	}
}

func TestTrackIsIdempotentAndUsesNormalizedLabels(t *testing.T) {
	metrics := New()
	done := metrics.Track(http.MethodGet, "/api/v1/items/42")
	gauge := metrics.inFlight.WithLabelValues("GET", UnknownRoute)
	if got := testutil.ToFloat64(gauge); got != 1 {
		t.Fatalf("in-flight gauge = %v, want 1", got)
	}
	done()
	done()
	if got := testutil.ToFloat64(gauge); got != 0 {
		t.Fatalf("in-flight gauge after duplicate completion = %v, want 0", got)
	}
}

func TestTrackAndObserveAreSafeForConcurrentUse(t *testing.T) {
	metrics := New()
	const requests = 100
	var waitGroup sync.WaitGroup
	waitGroup.Add(requests)
	for index := 0; index < requests; index++ {
		go func(index int) {
			defer waitGroup.Done()
			done := metrics.Track(http.MethodGet, "/api/v1/items/:id")
			metrics.Observe(http.MethodGet, "/api/v1/items/:id", http.StatusOK, time.Duration(index)*time.Microsecond)
			done()
		}(index)
	}
	waitGroup.Wait()

	if got := testutil.ToFloat64(metrics.inFlight.WithLabelValues("GET", "/api/v1/items/:id")); got != 0 {
		t.Fatalf("in-flight gauge = %v, want 0", got)
	}
	if got := testutil.ToFloat64(metrics.requests.WithLabelValues("GET", "/api/v1/items/:id", "2xx", "200")); got != requests {
		t.Fatalf("request count = %v, want %d", got, requests)
	}
}

func TestHandlerExposesIsolatedPrometheusMetrics(t *testing.T) {
	metrics := New()
	done := metrics.Track(http.MethodGet, "/api/v1/items/:id")
	defer done()
	metrics.Observe(http.MethodGet, "/api/v1/items/:id", http.StatusOK, 25*time.Millisecond)

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	for _, token := range []string{
		"# HELP http_server_requests_total",
		"# TYPE http_server_requests_total counter",
		"http_server_requests_total{method=\"GET\",route=\"/api/v1/items/:id\",status_class=\"2xx\",status_code=\"200\"} 1",
		"# HELP http_server_request_duration_seconds",
		"# TYPE http_server_request_duration_seconds histogram",
		"# HELP http_server_requests_in_flight",
		"# TYPE http_server_requests_in_flight gauge",
	} {
		if !strings.Contains(body, token) {
			t.Errorf("metrics exposition missing %q:\n%s", token, body)
		}
	}
	if strings.Contains(body, "/api/v1/items/42") || strings.Contains(body, "metrics?secret") {
		t.Errorf("metrics exposition contains an unnormalized route: %s", body)
	}
	if strings.Contains(body, "go_") || strings.Contains(body, "process_") {
		t.Errorf("metrics exposition unexpectedly includes runtime collectors: %s", body)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		t.Errorf("metrics content type = %q, want text/plain exposition", contentType)
	}
}

func TestMetricsPassPrometheusLint(t *testing.T) {
	metrics := New()
	done := metrics.Track(http.MethodGet, "/api/health")
	metrics.Observe(http.MethodGet, "/api/health", http.StatusOK, time.Millisecond)
	done()
	problems, err := testutil.GatherAndLint(metrics.Registry())
	if err != nil {
		t.Fatalf("lint metrics: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("Prometheus lint problems: %v", problems)
	}
}

func TestNilMetricsRemainSafeAtIntegrationSeams(t *testing.T) {
	var metrics *Metrics
	metrics.Observe(http.MethodGet, "/", http.StatusOK, time.Second)
	done := metrics.Track(http.MethodGet, "/")
	done()
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("nil metrics handler status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func metricFamily(t *testing.T, metrics *Metrics, name string) *dto.MetricFamily {
	t.Helper()
	families, err := metrics.Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	t.Fatalf("metric family %q not found", name)
	return nil
}

func labelsOf(metric *dto.Metric) map[string]string {
	labels := make(map[string]string, len(metric.Label))
	for _, label := range metric.Label {
		labels[label.GetName()] = label.GetValue()
	}
	return labels
}

// Package metrics provides an isolated Prometheus registry for HTTP server
// telemetry. It intentionally registers only application metrics; process and
// Go runtime collectors can be added by a composition root when desired.
package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// UnknownRoute is used when a router cannot provide a stable route pattern.
	// Keeping unmatched requests under one label prevents raw URLs from creating
	// unbounded time series.
	UnknownRoute = "unmatched"

	maxRouteBytes = 128

	requestsMetricName = "http_server_requests_total"
	durationMetricName = "http_server_request_duration_seconds"
	inFlightMetricName = "http_server_requests_in_flight"
)

var supportedMethods = map[string]struct{}{
	"CONNECT": {},
	"DELETE":  {},
	"GET":     {},
	"HEAD":    {},
	"OPTIONS": {},
	"PATCH":   {},
	"POST":    {},
	"PUT":     {},
	"TRACE":   {},
}

// Metrics owns a fresh Prometheus registry and the HTTP request collectors
// registered in it. A Metrics value is safe for concurrent use.
type Metrics struct {
	registry  *prometheus.Registry
	requests  *prometheus.CounterVec
	durations *prometheus.HistogramVec
	inFlight  *prometheus.GaugeVec
}

// New constructs an isolated registry containing HTTP request metrics. No
// global default registry is touched, which allows tests and multiple server
// instances in one process to remain independent.
func New() *Metrics {
	requests := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: requestsMetricName,
			Help: "Total number of completed HTTP requests.",
		},
		[]string{"method", "route", "status_class", "status_code"},
	)
	durations := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    durationMetricName,
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route", "status_class", "status_code"},
	)
	inFlight := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: inFlightMetricName,
			Help: "Number of HTTP requests currently in flight.",
		},
		[]string{"method", "route"},
	)

	registry := prometheus.NewRegistry()
	registry.MustRegister(requests, durations, inFlight)
	return &Metrics{
		registry:  registry,
		requests:  requests,
		durations: durations,
		inFlight:  inFlight,
	}
}

// Registry returns the isolated registry owned by m. Callers can compose it
// with another Gatherer or use it directly in a custom scrape handler.
func (m *Metrics) Registry() *prometheus.Registry {
	if m == nil {
		return nil
	}
	return m.registry
}

// Handler returns a Prometheus exposition handler backed by m's isolated
// registry. The handler does not expose Go runtime or process collectors.
func (m *Metrics) Handler() http.Handler {
	if m == nil || m.registry == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Track increments the bounded in-flight series for a request and returns an
// idempotent completion function. Callers should invoke the returned function
// with defer immediately after entering a request middleware.
func (m *Metrics) Track(method, route string) func() {
	if m == nil || m.inFlight == nil {
		return func() {}
	}
	gauge := m.inFlight.WithLabelValues(normalizeMethod(method), NormalizeRoute(route))
	gauge.Inc()
	var once sync.Once
	return func() {
		once.Do(gauge.Dec)
	}
}

// Observe records one completed request. Status labels are normalized to a
// finite HTTP code/class vocabulary, and negative durations are clamped to
// zero before recording.
func (m *Metrics) Observe(method, route string, statusCode int, duration time.Duration) {
	if m == nil || m.requests == nil || m.durations == nil {
		return
	}
	method = normalizeMethod(method)
	route = NormalizeRoute(route)
	statusClass, status := normalizeStatus(statusCode)
	if duration < 0 {
		duration = 0
	}
	labels := []string{method, route, statusClass, status}
	m.requests.WithLabelValues(labels...).Inc()
	m.durations.WithLabelValues(labels...).Observe(duration.Seconds())
}

// NormalizeRoute accepts a router's stable route pattern and collapses empty,
// malformed, query-bearing, or obviously raw paths to UnknownRoute. Gin's
// FullPath method is the intended input. Dynamic placeholders in either Gin
// form (":id", "*path") or OpenAPI form ("{id}") are retained/canonicalized;
// concrete numeric and UUID path segments are treated as raw.
func NormalizeRoute(route string) string {
	route = strings.TrimSpace(route)
	if route == "" || route == UnknownRoute {
		return UnknownRoute
	}
	if len(route) > maxRouteBytes || !utf8.ValidString(route) || route[0] != '/' {
		return UnknownRoute
	}
	if strings.ContainsAny(route, "?#\r\n\t") || strings.Contains(route, "//") {
		return UnknownRoute
	}
	if route == "/" {
		return route
	}
	if strings.HasSuffix(route, "/") {
		route = strings.TrimRight(route, "/")
	}
	segments := strings.Split(strings.TrimPrefix(route, "/"), "/")
	for index, segment := range segments {
		if segment == "" {
			return UnknownRoute
		}
		if segment == "." || segment == ".." {
			return UnknownRoute
		}
		switch {
		case strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*"):
			if !validPlaceholder(segment[1:]) {
				return UnknownRoute
			}
		case strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}"):
			name := segment[1 : len(segment)-1]
			if !validPlaceholder(name) {
				return UnknownRoute
			}
			segments[index] = ":" + name
		case !validLiteral(segment) || looksLikeRawSegment(segment):
			return UnknownRoute
		}
	}
	return "/" + strings.Join(segments, "/")
}

func normalizeMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if _, ok := supportedMethods[method]; !ok {
		return "OTHER"
	}
	return method
}

func normalizeStatus(statusCode int) (class, code string) {
	if statusCode < 100 || statusCode > 599 {
		return "unknown", "unknown"
	}
	return strconv.Itoa(statusCode/100) + "xx", strconv.Itoa(statusCode)
}

func validPlaceholder(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func validLiteral(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("._~-", rune(character)) {
			continue
		}
		return false
	}
	return value != ""
}

func looksLikeRawSegment(value string) bool {
	allDigits := true
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return true
	}
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for _, character := range value {
		if character == '-' {
			continue
		}
		if !isHexDigit(character) {
			return false
		}
	}
	return true
}

func isHexDigit(character rune) bool {
	return (character >= 'a' && character <= 'f') ||
		(character >= 'A' && character <= 'F') ||
		(character >= '0' && character <= '9')
}

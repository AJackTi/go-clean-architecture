package http

import (
	"net/http"
	"strings"
	"time"

	appmetrics "github.com/AJackTi/go-clean-architecture/pkg/metrics"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentationName is the stable OpenTelemetry instrumentation scope used
// by the HTTP composition adapter.
const InstrumentationName = "github.com/AJackTi/go-clean-architecture/internal/controller/http"

func metricsMiddleware(metricSet *appmetrics.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		method := requestMethod(c)
		route := matchedRoute(c)
		done := metricSet.Track(method, route)
		defer done()

		c.Next()

		metricSet.Observe(method, matchedRoute(c), responseStatus(c), time.Since(started))
	}
}

func tracingMiddleware(tracer trace.Tracer, propagator propagation.TextMapPropagator) gin.HandlerFunc {
	if propagator == nil {
		propagator = propagation.TraceContext{}
	}
	return func(c *gin.Context) {
		if c.Request == nil {
			c.Next()
			return
		}

		method := requestMethod(c)
		parent := propagator.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		ctx, span := tracer.Start(
			parent,
			method,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(attribute.String("http.request.method", method)),
		)
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		route := matchedRoute(c)
		status := responseStatus(c)
		span.SetName(strings.TrimSpace(method + " " + route))
		span.SetAttributes(
			attribute.String("http.route", route),
			attribute.Int("http.response.status_code", status),
			attribute.String("request.id", requestIDFromContext(c)),
		)
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, "internal server error")
		}
		span.End()
	}
}

func matchedRoute(c *gin.Context) string {
	if c == nil {
		return defaultRouteName
	}
	route := c.FullPath()
	if route == "" {
		return defaultRouteName
	}
	return route
}

func requestMethod(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return c.Request.Method
}

func responseStatus(c *gin.Context) int {
	if c == nil || c.Writer == nil {
		return http.StatusOK
	}
	status := c.Writer.Status()
	if status <= 0 {
		return http.StatusOK
	}
	return status
}

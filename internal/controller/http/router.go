// Package http composes the HTTP delivery adapters.
package http

import (
	"context"
	nethttp "net/http"
	"time"

	"github.com/AJackTi/go-clean-architecture/internal/item/httpapi"
	"github.com/AJackTi/go-clean-architecture/pkg/auth"
	"github.com/AJackTi/go-clean-architecture/pkg/metrics"
	"github.com/AJackTi/go-clean-architecture/pkg/ratelimit"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	livenessPath     = "/api/health"
	readinessPath    = "/api/healthz"
	metricsPath      = "/metrics"
	readinessTimeout = 2 * time.Second
)

// ReadinessCheck reports whether the application's required dependencies are
// available. The request context lets a failed client or server shutdown
// cancel an in-flight check.
type ReadinessCheck func(context.Context) error

type routerConfig struct {
	accessLog  accessLogger
	metrics    *metrics.Metrics
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	auth       auth.Authenticator
	limiter    *ratelimit.Limiter
}

// RouterOption configures optional transport-owned diagnostics without
// changing the Item service boundary.
type RouterOption func(*routerConfig)

// WithMetrics enables the Prometheus scrape endpoint and HTTP server metrics.
// A nil Metrics value leaves the endpoint disabled.
func WithMetrics(value *metrics.Metrics) RouterOption {
	return func(config *routerConfig) {
		if config != nil {
			config.metrics = value
		}
	}
}

// WithTracing enables W3C trace-context extraction and server spans. A nil
// tracer leaves tracing disabled. A nil propagator uses TraceContext only.
func WithTracing(tracer trace.Tracer, propagator propagation.TextMapPropagator) RouterOption {
	return func(config *routerConfig) {
		if config == nil {
			return
		}
		config.tracer = tracer
		config.propagator = propagator
	}
}

// WithAuthenticator protects versioned API routes with the supplied
// transport-neutral authenticator. Health and metrics endpoints remain
// reachable for deployment probes and private scrape infrastructure.
func WithAuthenticator(value auth.Authenticator) RouterOption {
	return func(config *routerConfig) {
		if config != nil {
			config.auth = value
		}
	}
}

// WithRateLimiter enables bounded in-process request limiting for versioned
// API routes. A nil limiter leaves rate limiting disabled.
func WithRateLimiter(value *ratelimit.Limiter) RouterOption {
	return func(config *routerConfig) {
		if config != nil {
			config.limiter = value
		}
	}
}

// NewRouter wires middleware, operational endpoints, and the versioned API.
func NewRouter(items httpapi.Service, readiness ReadinessCheck, options ...RouterOption) *gin.Engine {
	config := routerConfig{accessLog: processAccessLogger{}}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	return buildRouter(items, readiness, config)
}

func newRouter(items httpapi.Service, readiness ReadinessCheck, accessLog accessLogger, options ...RouterOption) *gin.Engine {
	config := routerConfig{accessLog: accessLog}
	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}
	return buildRouter(items, readiness, config)
}

func buildRouter(items httpapi.Service, readiness ReadinessCheck, config routerConfig) *gin.Engine {
	router := gin.New()
	// Route-shape errors should pass through the same middleware as every
	// other response. Gin's automatic trailing-slash/fixed-path redirects run
	// before the handler chain and would otherwise omit both the request ID and
	// access event.
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	middleware := []gin.HandlerFunc{observabilityMiddleware(config.accessLog)}
	if config.tracer != nil {
		middleware = append(middleware, tracingMiddleware(config.tracer, config.propagator))
	}
	if config.metrics != nil {
		middleware = append(middleware, metricsMiddleware(config.metrics))
	}
	middleware = append(middleware, sanitizedRecovery())
	router.Use(middleware...)
	// Keep the middleware mounted even when both optional controls are off: it
	// still attaches a bounded direct-peer scope for idempotent creates.
	router.Use(apiSecurityMiddleware(config.auth, config.limiter))
	_ = router.SetTrustedProxies(nil)

	router.GET(livenessPath, func(c *gin.Context) {
		c.JSON(nethttp.StatusOK, healthResponse{Status: "ok"})
	})
	router.GET(readinessPath, func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
		defer cancel()
		if readiness == nil || readiness(ctx) != nil {
			c.JSON(nethttp.StatusServiceUnavailable, healthResponse{Status: "unavailable"})
			return
		}
		c.JSON(nethttp.StatusOK, healthResponse{Status: "ok"})
	})
	if config.metrics != nil {
		router.GET(metricsPath, gin.WrapH(config.metrics.Handler()))
	}

	apiGroup := router.Group("/api/v1")
	httpapi.New(items).RegisterRoutes(apiGroup)
	return router
}

type healthResponse struct {
	Status string `json:"status"`
}

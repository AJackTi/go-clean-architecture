// Package telemetry provides an opt-in OpenTelemetry tracing provider.
//
// The package is deliberately independent from the application's composition
// root: callers choose when to install the returned provider as the process
// global with otel.SetTracerProvider. An empty endpoint is a supported local
// default and creates an SDK provider with no exporter or network activity.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	// DefaultServiceName is used when Config.ServiceName is empty. It follows
	// the OpenTelemetry convention without guessing a downstream product name.
	DefaultServiceName = "unknown_service"

	defaultTracePath    = "/v1/traces"
	maxServiceNameBytes = 128
	maxEnvironmentBytes = 128
	maxEndpointBytes    = 2048
)

// ErrNilContext indicates that a caller supplied a nil context to a lifecycle
// operation. Contexts are required so shutdown and exporter startup can be
// bounded by the composition root.
var ErrNilContext = errors.New("telemetry: context must not be nil")

// Config controls tracing setup.
//
// Endpoint accepts either a complete OTLP/HTTP URL (for example,
// "https://collector.example/v1/traces") or a host[:port][/path] value. A
// host-only value uses HTTPS unless Insecure is true. Complete HTTP URLs must
// set Insecure, and complete HTTPS URLs must leave it false; rejecting a
// mismatch prevents an accidental plaintext exporter.
//
// ServiceName is attached as the resource's service.name attribute and
// Environment, when non-empty, is attached as deployment.environment.name.
// SampleRatio is ParentBased with a TraceIDRatioBased root sampler and must be
// in [0, 1]. A zero ratio intentionally samples no root spans while still
// honoring sampled parent spans.
type Config struct {
	Endpoint    string
	Insecure    bool
	ServiceName string
	Environment string
	SampleRatio float64
}

// Validate checks Config without creating a provider or exporter.
func (c Config) Validate() error {
	_, err := normalizeConfig(c)
	return err
}

type normalizedConfig struct {
	serviceName string
	environment string
	sampleRatio float64
	endpointURL string
}

type exporterSettings struct {
	endpointURL string
}

type exporterFactory func(context.Context, exporterSettings) (sdktrace.SpanExporter, error)

// Provider owns an SDK tracer provider and, when configured, its OTLP/HTTP
// exporter. It is safe for concurrent use. Call Shutdown from the process
// lifecycle exactly as you would close a database pool or HTTP server.
type Provider struct {
	provider        *sdktrace.TracerProvider
	resource        *resource.Resource
	exporterEnabled bool

	shutdownOnce sync.Once
	shutdownErr  error
}

// New constructs an opt-in tracing provider. It does not mutate OpenTelemetry
// global state and does not contact the network during construction. When
// Config.Endpoint is empty, no exporter or span processor is created.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	return newProvider(ctx, cfg, newOTLPHTTPExporter)
}

// NewProvider is an explicit alias for New for composition roots that prefer
// provider-oriented naming.
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	return New(ctx, cfg)
}

func newProvider(ctx context.Context, cfg Config, makeExporter exporterFactory) (*Provider, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}

	attrs := []attribute.KeyValue{
		attribute.String("service.name", normalized.serviceName),
	}
	if normalized.environment != "" {
		attrs = append(attrs, attribute.String("deployment.environment.name", normalized.environment))
	}
	res, err := resource.New(
		ctx,
		resource.WithTelemetrySDK(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build resource: %w", err)
	}

	providerOptions := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(normalized.sampleRatio))),
	}
	enabled := normalized.endpointURL != ""
	if enabled {
		if makeExporter == nil {
			return nil, errors.New("telemetry: exporter factory must not be nil")
		}
		exporter, err := makeExporter(ctx, exporterSettings{endpointURL: normalized.endpointURL})
		if err != nil {
			return nil, fmt.Errorf("telemetry: create OTLP/HTTP exporter: %w", err)
		}
		if exporter == nil {
			return nil, errors.New("telemetry: exporter factory returned a nil exporter")
		}
		providerOptions = append(providerOptions, sdktrace.WithBatcher(exporter))
	}

	return &Provider{
		provider:        sdktrace.NewTracerProvider(providerOptions...),
		resource:        res,
		exporterEnabled: enabled,
	}, nil
}

func newOTLPHTTPExporter(ctx context.Context, settings exporterSettings) (sdktrace.SpanExporter, error) {
	return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(settings.endpointURL))
}

// Tracer returns a tracer from the owned SDK provider. A nil Provider yields a
// no-op tracer, which makes optional instrumentation safe during error paths.
func (p *Provider) Tracer(name string, options ...trace.TracerOption) trace.Tracer {
	if p == nil || p.provider == nil {
		return noop.NewTracerProvider().Tracer(name, options...)
	}
	return p.provider.Tracer(name, options...)
}

// TracerProvider exposes the concrete SDK provider for installation with
// otel.SetTracerProvider or for advanced SDK operations.
func (p *Provider) TracerProvider() *sdktrace.TracerProvider {
	if p == nil {
		return nil
	}
	return p.provider
}

// Resource returns the immutable resource attached to the SDK provider.
// A nil Provider returns nil.
func (p *Provider) Resource() *resource.Resource {
	if p == nil {
		return nil
	}
	return p.resource
}

// ExporterEnabled reports whether an OTLP exporter was configured.
func (p *Provider) ExporterEnabled() bool {
	return p != nil && p.exporterEnabled
}

// ForceFlush asks all registered span processors to export pending spans.
func (p *Provider) ForceFlush(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	if p == nil || p.provider == nil {
		return nil
	}
	return p.provider.ForceFlush(ctx)
}

// Shutdown flushes and closes the provider exactly once. A nil Provider is a
// safe no-op; a nil context is rejected so callers cannot accidentally create
// an unbounded exporter shutdown.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.provider == nil {
		return nil
	}
	if ctx == nil {
		return ErrNilContext
	}
	p.shutdownOnce.Do(func() {
		p.shutdownErr = p.provider.Shutdown(ctx)
	})
	return p.shutdownErr
}

func normalizeConfig(cfg Config) (normalizedConfig, error) {
	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = DefaultServiceName
	}
	if err := validateText("service name", serviceName); err != nil {
		return normalizedConfig{}, err
	}
	if len(serviceName) > maxServiceNameBytes {
		return normalizedConfig{}, fmt.Errorf("telemetry: service name must be at most %d bytes", maxServiceNameBytes)
	}
	environment := strings.TrimSpace(cfg.Environment)
	if err := validateText("environment", environment); err != nil {
		return normalizedConfig{}, err
	}
	if len(environment) > maxEnvironmentBytes {
		return normalizedConfig{}, fmt.Errorf("telemetry: environment must be at most %d bytes", maxEnvironmentBytes)
	}

	ratio := cfg.SampleRatio
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
		return normalizedConfig{}, fmt.Errorf("telemetry: sample ratio must be between 0 and 1 (got %v)", cfg.SampleRatio)
	}
	endpointURL, err := normalizeEndpoint(cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return normalizedConfig{}, err
	}
	return normalizedConfig{
		serviceName: serviceName,
		environment: environment,
		sampleRatio: ratio,
		endpointURL: endpointURL,
	}, nil
}

func validateText(label, value string) error {
	if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("telemetry: %s contains invalid control or UTF-8 data", label)
	}
	return nil
}

func normalizeEndpoint(raw string, insecure bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxEndpointBytes {
		return "", fmt.Errorf("telemetry: OTLP endpoint must be at most %d bytes", maxEndpointBytes)
	}
	if raw == "" {
		// An empty endpoint is the explicit opt-out. Insecure is ignored here so
		// a deployment can safely leave exporter settings disabled without
		// having to coordinate a second boolean setting.
		return "", nil
	}
	if strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
		return "", errors.New("telemetry: OTLP endpoint must not contain whitespace")
	}

	var endpoint *url.URL
	var err error
	if strings.Contains(raw, "://") {
		endpoint, err = url.Parse(raw)
		if err != nil {
			return "", errors.New("telemetry: invalid OTLP endpoint")
		}
		scheme := strings.ToLower(endpoint.Scheme)
		if scheme != "http" && scheme != "https" {
			return "", fmt.Errorf("telemetry: OTLP endpoint scheme must be http or https (got %q)", endpoint.Scheme)
		}
		if (scheme == "http") != insecure {
			return "", fmt.Errorf("telemetry: endpoint scheme and insecure flag disagree")
		}
		endpoint.Scheme = scheme
	} else {
		endpoint, err = url.Parse("https://" + raw)
		if err != nil {
			return "", errors.New("telemetry: invalid OTLP endpoint")
		}
		if insecure {
			endpoint.Scheme = "http"
		}
	}

	if endpoint.Host == "" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.ForceQuery || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Opaque != "" {
		return "", errors.New("telemetry: invalid OTLP endpoint")
	}
	if port := endpoint.Port(); port != "" {
		parsedPort, parseErr := strconv.Atoi(port)
		if parseErr != nil || parsedPort < 1 || parsedPort > 65535 {
			return "", fmt.Errorf("telemetry: invalid OTLP endpoint port %q", port)
		}
	}
	if endpoint.Path == "" || endpoint.Path == "/" {
		endpoint.Path = defaultTracePath
	}
	if !strings.HasPrefix(endpoint.Path, "/") {
		return "", errors.New("telemetry: invalid OTLP endpoint path")
	}
	endpoint.RawPath = ""
	return endpoint.String(), nil
}

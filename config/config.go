// Package config loads the small set of settings needed by the application.
//
// Configuration is intentionally environment-first.  A fresh checkout can
// run with the documented development defaults, while deployments can provide
// one explicit value per setting without relying on a working-directory
// relative YAML file.
package config

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultAppEnv               = "development"
	defaultHTTPPort             = "8080"
	defaultLogLevel             = "info"
	defaultDatabaseURL          = "postgres://localhost:5432/app?sslmode=disable"
	defaultMetricsEnabled       = false
	defaultOTELServiceName      = "github.com/AJackTi/go-clean-architecture"
	defaultOTELTracesSampler    = "parentbased_traceidratio"
	defaultOTELTracesSamplerArg = 0.1
	maxOTELServiceNameBytes     = 128
	maxOTELSamplerBytes         = 64
	maxOTLPEndpointBytes        = 2048
)

// Config contains the runtime settings required by the HTTP application.
// Values are deliberately flat so the composition root has one obvious
// source of truth for each dependency.
type Config struct {
	AppEnv                   string
	HTTPPort                 string
	LogLevel                 string
	DatabaseURL              string
	MetricsEnabled           bool
	OTELServiceName          string
	OTELExporterOTLPEndpoint string
	OTELExporterOTLPInsecure bool
	OTELTracesSampler        string
	OTELTracesSamplerArg     float64
}

// NewConfig is kept as the conventional constructor name used by existing
// callers.  Load is the preferred name for new code.
func NewConfig() (*Config, error) { return Load() }

// Load reads environment variables and applies safe development defaults.
// An explicitly empty environment variable is not treated as missing; it is
// validated and reported so a misspelled/empty deployment setting cannot be
// silently replaced by a local default.
func Load() (*Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (*Config, error) {
	for _, key := range []string{
		"APP_ENV",
		"HTTP_PORT",
		"LOG_LEVEL",
		"DATABASE_URL",
		"OTEL_SERVICE_NAME",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_TRACES_SAMPLER",
	} {
		if value, ok := lookup(key); ok {
			if err := validateText(key, value); err != nil {
				return nil, err
			}
		}
	}

	databaseURL, databaseURLSet := lookup("DATABASE_URL")
	if !databaseURLSet {
		databaseURL = defaultDatabaseURL
	}

	metricsEnabled, err := boolValueOrDefault(lookup, "METRICS_ENABLED", defaultMetricsEnabled)
	if err != nil {
		return nil, err
	}
	otelInsecure, err := boolValueOrDefault(lookup, "OTEL_EXPORTER_OTLP_INSECURE", false)
	if err != nil {
		return nil, err
	}
	otelSamplerArg, err := floatValueOrDefault(lookup, "OTEL_TRACES_SAMPLER_ARG", defaultOTELTracesSamplerArg)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		AppEnv:                   strings.TrimSpace(valueOrDefault(lookup, "APP_ENV", defaultAppEnv)),
		HTTPPort:                 strings.TrimSpace(valueOrDefault(lookup, "HTTP_PORT", defaultHTTPPort)),
		LogLevel:                 strings.TrimSpace(valueOrDefault(lookup, "LOG_LEVEL", defaultLogLevel)),
		DatabaseURL:              strings.TrimSpace(databaseURL),
		MetricsEnabled:           metricsEnabled,
		OTELServiceName:          strings.TrimSpace(valueOrDefault(lookup, "OTEL_SERVICE_NAME", defaultOTELServiceName)),
		OTELExporterOTLPEndpoint: strings.TrimSpace(valueOrDefault(lookup, "OTEL_EXPORTER_OTLP_ENDPOINT", "")),
		OTELExporterOTLPInsecure: otelInsecure,
		OTELTracesSampler:        strings.TrimSpace(valueOrDefault(lookup, "OTEL_TRACES_SAMPLER", defaultOTELTracesSampler)),
		OTELTracesSamplerArg:     otelSamplerArg,
	}

	if cfg.AppEnv == "production" && !databaseURLSet {
		return nil, fmt.Errorf("config: DATABASE_URL must be explicitly set in production")
	}
	if _, set := lookup("OTEL_SERVICE_NAME"); set && cfg.OTELServiceName == "" {
		return nil, fmt.Errorf("config: OTEL_SERVICE_NAME must not be empty")
	}
	if _, set := lookup("OTEL_TRACES_SAMPLER"); set && cfg.OTELTracesSampler == "" {
		return nil, fmt.Errorf("config: OTEL_TRACES_SAMPLER must not be empty")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func valueOrDefault(lookup func(string) (string, bool), key, fallback string) string {
	if value, ok := lookup(key); ok {
		return value
	}
	return fallback
}

func boolValueOrDefault(lookup func(string) (string, bool), key string, fallback bool) (bool, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	if err := validateText(key, value); err != nil {
		return false, err
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, fmt.Errorf("config: %s must be a boolean", key)
	}
	return parsed, nil
}

func floatValueOrDefault(lookup func(string) (string, bool), key string, fallback float64) (float64, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	if err := validateText(key, value); err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a number", key)
	}
	return parsed, nil
}

// Validate checks settings that would otherwise produce a confusing startup
// failure.  Database URL syntax is intentionally left to the database driver;
// this package only guarantees that it is present.
func (c Config) Validate() error {
	textSettings := []struct {
		name  string
		value string
	}{
		{name: "APP_ENV", value: c.AppEnv},
		{name: "HTTP_PORT", value: c.HTTPPort},
		{name: "LOG_LEVEL", value: c.LogLevel},
		{name: "DATABASE_URL", value: c.DatabaseURL},
		{name: "OTEL_SERVICE_NAME", value: c.OTELServiceName},
		{name: "OTEL_EXPORTER_OTLP_ENDPOINT", value: c.OTELExporterOTLPEndpoint},
		{name: "OTEL_TRACES_SAMPLER", value: c.OTELTracesSampler},
	}
	for _, setting := range textSettings {
		if err := validateText(setting.name, setting.value); err != nil {
			return err
		}
	}
	if len(c.OTELServiceName) > maxOTELServiceNameBytes {
		return fmt.Errorf("config: OTEL_SERVICE_NAME must be at most %d bytes", maxOTELServiceNameBytes)
	}
	if len(c.OTELTracesSampler) > maxOTELSamplerBytes {
		return fmt.Errorf("config: OTEL_TRACES_SAMPLER must be at most %d bytes", maxOTELSamplerBytes)
	}
	if len(c.OTELExporterOTLPEndpoint) > maxOTLPEndpointBytes {
		return fmt.Errorf("config: OTEL_EXPORTER_OTLP_ENDPOINT must be at most %d bytes", maxOTLPEndpointBytes)
	}

	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("config: DATABASE_URL must not be empty")
	}

	port, err := strconv.Atoi(strings.TrimSpace(c.HTTPPort))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("config: HTTP_PORT must be an integer between 1 and 65535 (got %q)", c.HTTPPort)
	}

	if strings.TrimSpace(c.AppEnv) == "" {
		return fmt.Errorf("config: APP_ENV must not be empty")
	}
	if strings.TrimSpace(c.LogLevel) == "" {
		return fmt.Errorf("config: LOG_LEVEL must not be empty")
	}

	sampler := strings.TrimSpace(c.OTELTracesSampler)
	if sampler != "" && sampler != defaultOTELTracesSampler {
		return fmt.Errorf("config: OTEL_TRACES_SAMPLER must be %q", defaultOTELTracesSampler)
	}
	if math.IsNaN(c.OTELTracesSamplerArg) || math.IsInf(c.OTELTracesSamplerArg, 0) ||
		c.OTELTracesSamplerArg < 0 || c.OTELTracesSamplerArg > 1 {
		return fmt.Errorf("config: OTEL_TRACES_SAMPLER_ARG must be between 0 and 1")
	}
	if err := validateOTLPEndpoint(c.OTELExporterOTLPEndpoint, c.OTELExporterOTLPInsecure); err != nil {
		return err
	}
	if strings.TrimSpace(c.AppEnv) == "production" {
		if c.OTELExporterOTLPInsecure {
			return fmt.Errorf("config: OTEL_EXPORTER_OTLP_INSECURE must be false in production")
		}
		if endpointUsesHTTP(c.OTELExporterOTLPEndpoint) {
			return fmt.Errorf("config: OTEL_EXPORTER_OTLP_ENDPOINT must use HTTPS in production")
		}
	}

	return nil
}

func validateText(name, value string) error {
	if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("config: %s contains invalid control or UTF-8 data", name)
	}
	return nil
}

func validateOTLPEndpoint(raw string, insecure bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.IndexFunc(raw, unicode.IsSpace) >= 0 {
		return fmt.Errorf("config: OTEL_EXPORTER_OTLP_ENDPOINT must not contain whitespace")
	}

	endpointValue := raw
	hasScheme := strings.Contains(raw, "://")
	if !hasScheme {
		endpointValue = "https://" + raw
	}
	endpoint, err := url.Parse(endpointValue)
	if err != nil {
		return fmt.Errorf("config: invalid OTEL_EXPORTER_OTLP_ENDPOINT")
	}

	scheme := strings.ToLower(endpoint.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("config: OTEL_EXPORTER_OTLP_ENDPOINT scheme must be http or https")
	}
	if endpoint.Host == "" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.ForceQuery || endpoint.RawQuery != "" ||
		endpoint.Fragment != "" || endpoint.Opaque != "" {
		return fmt.Errorf("config: invalid OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if port := endpoint.Port(); port != "" {
		parsedPort, parseErr := strconv.Atoi(port)
		if parseErr != nil || parsedPort < 1 || parsedPort > 65535 {
			return fmt.Errorf("config: invalid OTEL_EXPORTER_OTLP_ENDPOINT port")
		}
	}
	// A host-only value inherits the deployment's insecure flag: HTTPS is the
	// safe default, while an explicit insecure opt-in selects HTTP.  For a
	// complete URL the scheme is authoritative and must agree with the flag.
	if hasScheme && (scheme == "http") != insecure {
		return fmt.Errorf("config: OTEL_EXPORTER_OTLP_ENDPOINT scheme and OTEL_EXPORTER_OTLP_INSECURE disagree")
	}
	return nil
}

func endpointUsesHTTP(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "http://")
}

// Package config loads the small set of settings needed by the application.
//
// Configuration is intentionally environment-first.  A fresh checkout can
// run with the documented development defaults, while deployments can provide
// one explicit value per setting without relying on a working-directory
// relative YAML file.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
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
	defaultAuthEnabled          = false
	defaultRateLimitEnabled     = false
	defaultRateLimitRPS         = 10.0
	defaultRateLimitBurst       = 20
	defaultRateLimitMaxClients  = 10000
	defaultRateLimitIdleTTL     = 10 * time.Minute
	defaultCursorSigningKey     = ""
	developmentCursorKey        = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	maxOTELServiceNameBytes     = 128
	maxOTELSamplerBytes         = 64
	maxOTLPEndpointBytes        = 2048
	maxAuthDigestBytes          = sha256.Size * 2
	maxRateLimitRPS             = 100000.0
	maxRateLimitBurst           = 100000
	maxRateLimitMaxClients      = 1000000
	maxRateLimitIdleTTL         = 24 * time.Hour
	maxCursorSigningKeyBytes    = sha256.Size
)

// Config contains the runtime settings required by the HTTP application.
// Values are deliberately flat so the composition root has one obvious
// source of truth for each dependency.
type Config struct {
	AppEnv                     string
	HTTPPort                   string
	LogLevel                   string
	DatabaseURL                string
	MetricsEnabled             bool
	OTELServiceName            string
	OTELExporterOTLPEndpoint   string
	OTELExporterOTLPInsecure   bool
	OTELTracesSampler          string
	OTELTracesSamplerArg       float64
	AuthEnabled                bool
	AuthBearerTokenSHA256      string
	RateLimitEnabled           bool
	RateLimitRequestsPerSecond float64
	RateLimitBurst             int
	RateLimitMaxClients        int
	RateLimitIdleTTL           time.Duration
	CursorSigningKey           string
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
		"AUTH_BEARER_TOKEN_SHA256",
		"RATE_LIMIT_IDLE_TTL",
		"CURSOR_SIGNING_KEY",
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
	authEnabled, err := boolValueOrDefault(lookup, "AUTH_ENABLED", defaultAuthEnabled)
	if err != nil {
		return nil, err
	}
	authDigest := strings.ToLower(strings.TrimSpace(valueOrDefault(lookup, "AUTH_BEARER_TOKEN_SHA256", "")))
	rateLimitEnabled, err := boolValueOrDefault(lookup, "RATE_LIMIT_ENABLED", defaultRateLimitEnabled)
	if err != nil {
		return nil, err
	}
	rateLimitRPS, err := floatValueOrDefault(lookup, "RATE_LIMIT_REQUESTS_PER_SECOND", defaultRateLimitRPS)
	if err != nil {
		return nil, err
	}
	rateLimitBurst, err := intValueOrDefault(lookup, "RATE_LIMIT_BURST", defaultRateLimitBurst)
	if err != nil {
		return nil, err
	}
	rateLimitMaxClients, err := intValueOrDefault(lookup, "RATE_LIMIT_MAX_CLIENTS", defaultRateLimitMaxClients)
	if err != nil {
		return nil, err
	}
	rateLimitIdleTTL, err := durationValueOrDefault(lookup, "RATE_LIMIT_IDLE_TTL", defaultRateLimitIdleTTL)
	if err != nil {
		return nil, err
	}
	cursorSigningKey, cursorSigningKeySet := lookup("CURSOR_SIGNING_KEY")
	if !cursorSigningKeySet {
		cursorSigningKey = defaultCursorSigningKey
	}
	cursorSigningKey = strings.ToLower(strings.TrimSpace(cursorSigningKey))

	cfg := &Config{
		AppEnv:                     strings.TrimSpace(valueOrDefault(lookup, "APP_ENV", defaultAppEnv)),
		HTTPPort:                   strings.TrimSpace(valueOrDefault(lookup, "HTTP_PORT", defaultHTTPPort)),
		LogLevel:                   strings.TrimSpace(valueOrDefault(lookup, "LOG_LEVEL", defaultLogLevel)),
		DatabaseURL:                strings.TrimSpace(databaseURL),
		MetricsEnabled:             metricsEnabled,
		OTELServiceName:            strings.TrimSpace(valueOrDefault(lookup, "OTEL_SERVICE_NAME", defaultOTELServiceName)),
		OTELExporterOTLPEndpoint:   strings.TrimSpace(valueOrDefault(lookup, "OTEL_EXPORTER_OTLP_ENDPOINT", "")),
		OTELExporterOTLPInsecure:   otelInsecure,
		OTELTracesSampler:          strings.TrimSpace(valueOrDefault(lookup, "OTEL_TRACES_SAMPLER", defaultOTELTracesSampler)),
		OTELTracesSamplerArg:       otelSamplerArg,
		AuthEnabled:                authEnabled,
		AuthBearerTokenSHA256:      authDigest,
		RateLimitEnabled:           rateLimitEnabled,
		RateLimitRequestsPerSecond: rateLimitRPS,
		RateLimitBurst:             rateLimitBurst,
		RateLimitMaxClients:        rateLimitMaxClients,
		RateLimitIdleTTL:           rateLimitIdleTTL,
		CursorSigningKey:           cursorSigningKey,
	}

	if cfg.AppEnv == "production" && !databaseURLSet {
		return nil, fmt.Errorf("config: DATABASE_URL must be explicitly set in production")
	}
	if cfg.AppEnv == "production" && !cursorSigningKeySet {
		return nil, fmt.Errorf("config: CURSOR_SIGNING_KEY must be explicitly set in production")
	}
	if cfg.AppEnv == "production" && cfg.CursorSigningKey == developmentCursorKey {
		return nil, fmt.Errorf("config: CURSOR_SIGNING_KEY must not use the development placeholder in production")
	}
	if cursorSigningKeySet && cfg.CursorSigningKey == "" {
		return nil, fmt.Errorf("config: CURSOR_SIGNING_KEY must not be empty")
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

func intValueOrDefault(lookup func(string) (string, bool), key string, fallback int) (int, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	if err := validateText(key, value); err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer", key)
	}
	return parsed, nil
}

func durationValueOrDefault(lookup func(string) (string, bool), key string, fallback time.Duration) (time.Duration, error) {
	value, ok := lookup(key)
	if !ok {
		return fallback, nil
	}
	if err := validateText(key, value); err != nil {
		return 0, err
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a duration", key)
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
		{name: "AUTH_BEARER_TOKEN_SHA256", value: c.AuthBearerTokenSHA256},
		{name: "CURSOR_SIGNING_KEY", value: c.CursorSigningKey},
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
	if c.AuthBearerTokenSHA256 != "" {
		if len(c.AuthBearerTokenSHA256) != maxAuthDigestBytes {
			return fmt.Errorf("config: AUTH_BEARER_TOKEN_SHA256 must be a %d-character SHA-256 hex digest", maxAuthDigestBytes)
		}
		if _, err := hex.DecodeString(c.AuthBearerTokenSHA256); err != nil {
			return fmt.Errorf("config: AUTH_BEARER_TOKEN_SHA256 must be hexadecimal")
		}
	}
	cursorSigningKey := strings.TrimSpace(c.CursorSigningKey)
	if cursorSigningKey != "" {
		if len(cursorSigningKey) != maxCursorSigningKeyBytes*2 {
			return fmt.Errorf("config: CURSOR_SIGNING_KEY must be a %d-character SHA-256 hex key", maxCursorSigningKeyBytes*2)
		}
		if _, err := hex.DecodeString(cursorSigningKey); err != nil {
			return fmt.Errorf("config: CURSOR_SIGNING_KEY must be hexadecimal")
		}
		// Validate can be called directly on a Config literal, bypassing load's
		// lowercase normalization. Compare canonically so casing/edge whitespace
		// cannot bypass the production placeholder guard.
		if strings.TrimSpace(c.AppEnv) == "production" && strings.EqualFold(cursorSigningKey, developmentCursorKey) {
			return fmt.Errorf("config: CURSOR_SIGNING_KEY must not use the development placeholder in production")
		}
	} else if strings.TrimSpace(c.AppEnv) == "production" {
		return fmt.Errorf("config: CURSOR_SIGNING_KEY must be set in production")
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
	if c.AuthEnabled && c.AuthBearerTokenSHA256 == "" {
		return fmt.Errorf("config: AUTH_BEARER_TOKEN_SHA256 must be set when AUTH_ENABLED is true")
	}
	if c.RateLimitEnabled {
		if math.IsNaN(c.RateLimitRequestsPerSecond) || math.IsInf(c.RateLimitRequestsPerSecond, 0) ||
			c.RateLimitRequestsPerSecond <= 0 || c.RateLimitRequestsPerSecond > maxRateLimitRPS {
			return fmt.Errorf("config: RATE_LIMIT_REQUESTS_PER_SECOND must be greater than 0 and at most %g", maxRateLimitRPS)
		}
		if c.RateLimitBurst < 1 || c.RateLimitBurst > maxRateLimitBurst {
			return fmt.Errorf("config: RATE_LIMIT_BURST must be between 1 and %d", maxRateLimitBurst)
		}
		if c.RateLimitMaxClients < 1 || c.RateLimitMaxClients > maxRateLimitMaxClients {
			return fmt.Errorf("config: RATE_LIMIT_MAX_CLIENTS must be between 1 and %d", maxRateLimitMaxClients)
		}
		if c.RateLimitIdleTTL < time.Second || c.RateLimitIdleTTL > maxRateLimitIdleTTL {
			return fmt.Errorf("config: RATE_LIMIT_IDLE_TTL must be between 1s and %s", maxRateLimitIdleTTL)
		}
	} else {
		// Zero values keep old Config literals source-compatible. Non-zero
		// values are still checked when supplied so typos cannot silently
		// become an invalid limiter after a later enablement.
		if math.IsNaN(c.RateLimitRequestsPerSecond) || math.IsInf(c.RateLimitRequestsPerSecond, 0) ||
			c.RateLimitRequestsPerSecond < 0 || c.RateLimitRequestsPerSecond > maxRateLimitRPS {
			return fmt.Errorf("config: RATE_LIMIT_REQUESTS_PER_SECOND is outside its allowed range")
		}
		if c.RateLimitBurst < 0 || c.RateLimitBurst > maxRateLimitBurst {
			return fmt.Errorf("config: RATE_LIMIT_BURST is outside its allowed range")
		}
		if c.RateLimitMaxClients < 0 || c.RateLimitMaxClients > maxRateLimitMaxClients {
			return fmt.Errorf("config: RATE_LIMIT_MAX_CLIENTS is outside its allowed range")
		}
		if c.RateLimitIdleTTL < 0 || c.RateLimitIdleTTL > maxRateLimitIdleTTL {
			return fmt.Errorf("config: RATE_LIMIT_IDLE_TTL is outside its allowed range")
		}
	}

	return nil
}

// CursorSigningKeyBytes returns a defensive decoded copy for the composition
// root. An empty development value means cursor pagination is not configured;
// production validation rejects that state before this method is used.
func (c Config) CursorSigningKeyBytes() ([]byte, error) {
	value := strings.TrimSpace(c.CursorSigningKey)
	if value == "" {
		return nil, nil
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != maxCursorSigningKeyBytes {
		return nil, fmt.Errorf("config: invalid CURSOR_SIGNING_KEY")
	}
	return append([]byte(nil), decoded...), nil
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

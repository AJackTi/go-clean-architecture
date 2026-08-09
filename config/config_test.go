package config

import (
	"math"
	"strings"
	"testing"
)

func TestLoadUsesDevelopmentDefaults(t *testing.T) {
	lookup := func(string) (string, bool) { return "", false }

	cfg, err := load(lookup)
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}

	if cfg.AppEnv != defaultAppEnv {
		t.Errorf("AppEnv = %q, want %q", cfg.AppEnv, defaultAppEnv)
	}
	if cfg.HTTPPort != defaultHTTPPort {
		t.Errorf("HTTPPort = %q, want %q", cfg.HTTPPort, defaultHTTPPort)
	}
	if cfg.LogLevel != defaultLogLevel {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, defaultLogLevel)
	}
	if cfg.DatabaseURL != defaultDatabaseURL {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, defaultDatabaseURL)
	}
	if cfg.MetricsEnabled != defaultMetricsEnabled {
		t.Errorf("MetricsEnabled = %t, want %t", cfg.MetricsEnabled, defaultMetricsEnabled)
	}
	if cfg.OTELServiceName != defaultOTELServiceName {
		t.Errorf("OTELServiceName = %q, want %q", cfg.OTELServiceName, defaultOTELServiceName)
	}
	if cfg.OTELExporterOTLPEndpoint != "" {
		t.Errorf("OTELExporterOTLPEndpoint = %q, want empty", cfg.OTELExporterOTLPEndpoint)
	}
	if cfg.OTELExporterOTLPInsecure {
		t.Error("OTELExporterOTLPInsecure = true, want false")
	}
	if cfg.OTELTracesSampler != defaultOTELTracesSampler {
		t.Errorf("OTELTracesSampler = %q, want %q", cfg.OTELTracesSampler, defaultOTELTracesSampler)
	}
	if cfg.OTELTracesSamplerArg != defaultOTELTracesSamplerArg {
		t.Errorf("OTELTracesSamplerArg = %v, want %v", cfg.OTELTracesSamplerArg, defaultOTELTracesSamplerArg)
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	values := map[string]string{
		"APP_ENV":                     "test",
		"HTTP_PORT":                   "18080",
		"LOG_LEVEL":                   "debug",
		"DATABASE_URL":                "postgres://tester@db:5432/test?sslmode=disable",
		"METRICS_ENABLED":             "true",
		"OTEL_SERVICE_NAME":           "orders-api",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318/v1/traces",
		"OTEL_EXPORTER_OTLP_INSECURE": "true",
		"OTEL_TRACES_SAMPLER":         defaultOTELTracesSampler,
		"OTEL_TRACES_SAMPLER_ARG":     "0.25",
	}
	lookup := func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}

	cfg, err := load(lookup)
	if err != nil {
		t.Fatalf("load environment: %v", err)
	}

	if cfg.AppEnv != values["APP_ENV"] || cfg.HTTPPort != values["HTTP_PORT"] ||
		cfg.LogLevel != values["LOG_LEVEL"] || cfg.DatabaseURL != values["DATABASE_URL"] ||
		!cfg.MetricsEnabled || cfg.OTELServiceName != values["OTEL_SERVICE_NAME"] ||
		cfg.OTELExporterOTLPEndpoint != values["OTEL_EXPORTER_OTLP_ENDPOINT"] ||
		!cfg.OTELExporterOTLPInsecure || cfg.OTELTracesSampler != defaultOTELTracesSampler ||
		cfg.OTELTracesSamplerArg != 0.25 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]string
	}{
		{name: "empty database url", values: map[string]string{"DATABASE_URL": ""}},
		{name: "empty port", values: map[string]string{"HTTP_PORT": ""}},
		{name: "non numeric port", values: map[string]string{"HTTP_PORT": "http"}},
		{name: "port below range", values: map[string]string{"HTTP_PORT": "0"}},
		{name: "port above range", values: map[string]string{"HTTP_PORT": "65536"}},
		{name: "empty app environment", values: map[string]string{"APP_ENV": ""}},
		{name: "empty log level", values: map[string]string{"LOG_LEVEL": ""}},
		{name: "invalid metrics flag", values: map[string]string{"METRICS_ENABLED": "sometimes"}},
		{name: "invalid insecure flag", values: map[string]string{"OTEL_EXPORTER_OTLP_INSECURE": "sometimes"}},
		{name: "empty service name", values: map[string]string{"OTEL_SERVICE_NAME": ""}},
		{name: "empty sampler", values: map[string]string{"OTEL_TRACES_SAMPLER": ""}},
		{name: "unsupported sampler", values: map[string]string{"OTEL_TRACES_SAMPLER": "always_on"}},
		{name: "non numeric sampler argument", values: map[string]string{"OTEL_TRACES_SAMPLER_ARG": "many"}},
		{name: "negative sampler argument", values: map[string]string{"OTEL_TRACES_SAMPLER_ARG": "-0.01"}},
		{name: "sampler argument above one", values: map[string]string{"OTEL_TRACES_SAMPLER_ARG": "1.01"}},
		{name: "NaN sampler argument", values: map[string]string{"OTEL_TRACES_SAMPLER_ARG": "NaN"}},
		{name: "infinite sampler argument", values: map[string]string{"OTEL_TRACES_SAMPLER_ARG": "+Inf"}},
		{name: "oversized service name", values: map[string]string{"OTEL_SERVICE_NAME": strings.Repeat("a", maxOTELServiceNameBytes+1)}},
		{name: "oversized sampler", values: map[string]string{"OTEL_TRACES_SAMPLER": strings.Repeat("a", maxOTELSamplerBytes+1)}},
		{name: "oversized endpoint", values: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://" + strings.Repeat("a", maxOTLPEndpointBytes)}},
		{name: "unsupported endpoint scheme", values: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "grpc://collector:4317"}},
		{name: "endpoint without host", values: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": ":4318"}},
		{name: "endpoint with credentials", values: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://user@collector:4318"}},
		{name: "endpoint with query", values: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://collector:4318?token=secret"}},
		{name: "endpoint with empty query", values: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://collector:4318?"}},
		{name: "endpoint with fragment", values: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://collector:4318#traces"}},
		{name: "endpoint with invalid port", values: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "https://collector:99999"}},
		{name: "HTTP endpoint without insecure", values: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318"}},
		{name: "HTTPS endpoint with insecure", values: map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "https://collector:4318",
			"OTEL_EXPORTER_OTLP_INSECURE": "true",
		}},
		{name: "control character", values: map[string]string{"OTEL_SERVICE_NAME": "orders\napi"}},
		{name: "invalid UTF-8", values: map[string]string{"OTEL_SERVICE_NAME": string([]byte{0xff})}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lookup := func(key string) (string, bool) {
				value, ok := test.values[key]
				return value, ok
			}
			if _, err := load(lookup); err == nil {
				t.Fatal("load succeeded; want validation error")
			}
		})
	}
}

func TestConfigValidateAcceptsTelemetryBoundaries(t *testing.T) {
	for _, ratio := range []float64{0, 0.1, 1} {
		cfg := validConfig()
		cfg.OTELTracesSampler = defaultOTELTracesSampler
		cfg.OTELTracesSamplerArg = ratio
		cfg.OTELExporterOTLPEndpoint = "collector.example:4318/custom/traces"
		if err := cfg.Validate(); err != nil {
			t.Errorf("ratio %v: validate = %v", ratio, err)
		}
	}
}

func TestConfigValidateAcceptsHostOnlyPlaintextTelemetryOutsideProduction(t *testing.T) {
	cfg := validConfig()
	cfg.OTELExporterOTLPEndpoint = "collector.example:4318"
	cfg.OTELExporterOTLPInsecure = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want host-only insecure endpoint to be accepted outside production", err)
	}
}

func TestConfigValidatePreservesLegacyLiteralCompatibility(t *testing.T) {
	cfg := Config{
		AppEnv:      defaultAppEnv,
		HTTPPort:    defaultHTTPPort,
		LogLevel:    defaultLogLevel,
		DatabaseURL: defaultDatabaseURL,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("legacy zero-value telemetry fields: validate = %v", err)
	}
}

func TestConfigValidateRejectsInvalidTelemetryValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "negative ratio", mutate: func(cfg *Config) { cfg.OTELTracesSamplerArg = -0.01 }},
		{name: "ratio above one", mutate: func(cfg *Config) { cfg.OTELTracesSamplerArg = 1.01 }},
		{name: "NaN ratio", mutate: func(cfg *Config) { cfg.OTELTracesSamplerArg = math.NaN() }},
		{name: "positive infinity ratio", mutate: func(cfg *Config) { cfg.OTELTracesSamplerArg = math.Inf(1) }},
		{name: "negative infinity ratio", mutate: func(cfg *Config) { cfg.OTELTracesSamplerArg = math.Inf(-1) }},
		{name: "invalid sampler", mutate: func(cfg *Config) { cfg.OTELTracesSampler = "traceidratio" }},
		{name: "malformed endpoint", mutate: func(cfg *Config) { cfg.OTELExporterOTLPEndpoint = "://collector" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := validConfig()
			test.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() succeeded; want error")
			}
		})
	}
}

func TestConfigValidateRejectsInvalidText(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config, string)
	}{
		{name: "app environment", mutate: func(cfg *Config, value string) { cfg.AppEnv = value }},
		{name: "HTTP port", mutate: func(cfg *Config, value string) { cfg.HTTPPort = value }},
		{name: "log level", mutate: func(cfg *Config, value string) { cfg.LogLevel = value }},
		{name: "database URL", mutate: func(cfg *Config, value string) { cfg.DatabaseURL = value }},
		{name: "service name", mutate: func(cfg *Config, value string) { cfg.OTELServiceName = value }},
		{name: "OTLP endpoint", mutate: func(cfg *Config, value string) { cfg.OTELExporterOTLPEndpoint = value }},
		{name: "traces sampler", mutate: func(cfg *Config, value string) { cfg.OTELTracesSampler = value }},
	}
	invalidValues := []string{"value\x00suffix", "value\nsuffix", string([]byte{'v', 0xff})}

	for _, test := range tests {
		for _, value := range invalidValues {
			name := "control"
			if !strings.Contains(value, "\x00") && !strings.Contains(value, "\n") {
				name = "invalid UTF-8"
			}
			t.Run(test.name+"/"+name, func(t *testing.T) {
				cfg := validConfig()
				test.mutate(&cfg, value)
				if err := cfg.Validate(); err == nil {
					t.Fatal("Validate() succeeded; want error")
				}
			})
		}
	}
}

func TestLoadRejectsPlaintextTelemetryInProduction(t *testing.T) {
	tests := []map[string]string{
		{
			"APP_ENV":                     "production",
			"DATABASE_URL":                defaultDatabaseURL,
			"OTEL_EXPORTER_OTLP_ENDPOINT": "collector:4318",
			"OTEL_EXPORTER_OTLP_INSECURE": "true",
		},
		{
			"APP_ENV":                     "production",
			"DATABASE_URL":                defaultDatabaseURL,
			"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318",
			"OTEL_EXPORTER_OTLP_INSECURE": "true",
		},
		{
			"APP_ENV":                     "production",
			"DATABASE_URL":                defaultDatabaseURL,
			"OTEL_EXPORTER_OTLP_INSECURE": "true",
		},
	}

	for index, values := range tests {
		_, err := load(func(key string) (string, bool) {
			value, ok := values[key]
			return value, ok
		})
		if err == nil {
			t.Errorf("case %d: load succeeded; want production security error", index)
		}
	}
}

func TestLoadAcceptsSecureTelemetryInProduction(t *testing.T) {
	values := map[string]string{
		"APP_ENV":                     "production",
		"DATABASE_URL":                defaultDatabaseURL,
		"OTEL_EXPORTER_OTLP_ENDPOINT": "https://collector:4318/v1/traces",
	}
	if _, err := load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}); err != nil {
		t.Fatalf("load secure production telemetry: %v", err)
	}
}

func TestConfigValidateAcceptsBoundaryPorts(t *testing.T) {
	for _, port := range []string{"1", "8080", "65535"} {
		cfg := Config{
			AppEnv:      defaultAppEnv,
			HTTPPort:    port,
			LogLevel:    defaultLogLevel,
			DatabaseURL: defaultDatabaseURL,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("port %s: validate = %v", port, err)
		}
	}
}

func TestLoadRequiresExplicitProductionDatabaseURL(t *testing.T) {
	lookup := func(key string) (string, bool) {
		if key == "APP_ENV" {
			return "production", true
		}
		return "", false
	}
	if _, err := load(lookup); err == nil {
		t.Fatal("production config without DATABASE_URL succeeded")
	}
}

func TestLoadNormalizesWhitespace(t *testing.T) {
	values := map[string]string{
		"APP_ENV":                     " test ",
		"HTTP_PORT":                   " 18080 ",
		"LOG_LEVEL":                   " info ",
		"DATABASE_URL":                " postgres://localhost/test ",
		"METRICS_ENABLED":             " true ",
		"OTEL_SERVICE_NAME":           " orders-api ",
		"OTEL_EXPORTER_OTLP_ENDPOINT": " http://collector:4318 ",
		"OTEL_EXPORTER_OTLP_INSECURE": " true ",
		"OTEL_TRACES_SAMPLER":         " parentbased_traceidratio ",
		"OTEL_TRACES_SAMPLER_ARG":     " 0.5 ",
	}
	cfg, err := load(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.AppEnv != "test" || cfg.HTTPPort != "18080" || cfg.LogLevel != "info" ||
		cfg.DatabaseURL != "postgres://localhost/test" || !cfg.MetricsEnabled ||
		cfg.OTELServiceName != "orders-api" || cfg.OTELExporterOTLPEndpoint != "http://collector:4318" ||
		!cfg.OTELExporterOTLPInsecure || cfg.OTELTracesSampler != defaultOTELTracesSampler ||
		cfg.OTELTracesSamplerArg != 0.5 {
		t.Fatalf("config was not normalized: %#v", cfg)
	}
}

func validConfig() Config {
	return Config{
		AppEnv:               defaultAppEnv,
		HTTPPort:             defaultHTTPPort,
		LogLevel:             defaultLogLevel,
		DatabaseURL:          defaultDatabaseURL,
		OTELServiceName:      defaultOTELServiceName,
		OTELTracesSampler:    defaultOTELTracesSampler,
		OTELTracesSamplerArg: defaultOTELTracesSamplerArg,
	}
}

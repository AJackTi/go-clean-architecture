package config

import (
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
}

func TestLoadReadsEnvironment(t *testing.T) {
	values := map[string]string{
		"APP_ENV":      "test",
		"HTTP_PORT":    "18080",
		"LOG_LEVEL":    "debug",
		"DATABASE_URL": "postgres://tester@db:5432/test?sslmode=disable",
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
		cfg.LogLevel != values["LOG_LEVEL"] || cfg.DatabaseURL != values["DATABASE_URL"] {
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

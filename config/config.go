// Package config loads the small set of settings needed by the application.
//
// Configuration is intentionally environment-first.  A fresh checkout can
// run with the documented development defaults, while deployments can provide
// one explicit value per setting without relying on a working-directory
// relative YAML file.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	defaultAppEnv      = "development"
	defaultHTTPPort    = "8080"
	defaultLogLevel    = "info"
	defaultDatabaseURL = "postgres://localhost:5432/app?sslmode=disable"
)

// Config contains the runtime settings required by the HTTP application.
// Values are deliberately flat so the composition root has one obvious
// source of truth for each dependency.
type Config struct {
	AppEnv      string
	HTTPPort    string
	LogLevel    string
	DatabaseURL string
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
	databaseURL, databaseURLSet := lookup("DATABASE_URL")
	if !databaseURLSet {
		databaseURL = defaultDatabaseURL
	}
	cfg := &Config{
		AppEnv:      strings.TrimSpace(valueOrDefault(lookup, "APP_ENV", defaultAppEnv)),
		HTTPPort:    strings.TrimSpace(valueOrDefault(lookup, "HTTP_PORT", defaultHTTPPort)),
		LogLevel:    strings.TrimSpace(valueOrDefault(lookup, "LOG_LEVEL", defaultLogLevel)),
		DatabaseURL: strings.TrimSpace(databaseURL),
	}

	if cfg.AppEnv == "production" && !databaseURLSet {
		return nil, fmt.Errorf("config: DATABASE_URL must be explicitly set in production")
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

// Validate checks settings that would otherwise produce a confusing startup
// failure.  Database URL syntax is intentionally left to the database driver;
// this package only guarantees that it is present.
func (c Config) Validate() error {
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

	return nil
}

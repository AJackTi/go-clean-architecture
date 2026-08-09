package app

import (
	"context"
	"testing"

	"github.com/AJackTi/go-clean-architecture/config"
)

func TestRunContextRejectsInvalidInputsBeforeStartingDependencies(t *testing.T) {
	if err := RunContext(nilContext(), nil); err == nil {
		t.Fatal("RunContext(nil, nil) succeeded")
	}
	if err := RunContext(context.Background(), nil); err == nil {
		t.Fatal("RunContext with nil config succeeded")
	}
	cfg := &config.Config{
		AppEnv:      "test",
		HTTPPort:    "invalid",
		LogLevel:    "info",
		DatabaseURL: "postgres://localhost/test",
	}
	if err := RunContext(context.Background(), cfg); err == nil {
		t.Fatal("RunContext with invalid config succeeded")
	}
}

func nilContext() context.Context { return nil }

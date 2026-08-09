// Package app owns the application composition root and lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AJackTi/go-clean-architecture/config"
	httpcontroller "github.com/AJackTi/go-clean-architecture/internal/controller/http"
	"github.com/AJackTi/go-clean-architecture/internal/item"
	itempostgres "github.com/AJackTi/go-clean-architecture/internal/item/postgres"
	"github.com/AJackTi/go-clean-architecture/pkg/httpserver"
	"github.com/AJackTi/go-clean-architecture/pkg/logger"
	"github.com/AJackTi/go-clean-architecture/pkg/metrics"
	"github.com/AJackTi/go-clean-architecture/pkg/telemetry"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/propagation"
)

const (
	databaseStartupTimeout   = 10 * time.Second
	telemetryShutdownTimeout = 5 * time.Second
)

// Run installs signal handling and runs the application until it is stopped.
func Run(cfg *config.Config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return RunContext(ctx, cfg)
}

// RunContext is the lifecycle entry point used by main and by integration
// tests. It fails fast when the database is unavailable, then performs a
// bounded graceful HTTP shutdown when the context is cancelled.
func RunContext(ctx context.Context, cfg *config.Config) (runErr error) {
	if ctx == nil {
		return errors.New("app: nil context")
	}
	if cfg == nil {
		return errors.New("app: nil config")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	tracing, err := telemetry.New(ctx, telemetry.Config{
		Endpoint:    cfg.OTELExporterOTLPEndpoint,
		Insecure:    cfg.OTELExporterOTLPInsecure,
		ServiceName: cfg.OTELServiceName,
		Environment: cfg.AppEnv,
		SampleRatio: cfg.OTELTracesSamplerArg,
	})
	if err != nil {
		return fmt.Errorf("app: initialize telemetry: %w", err)
	}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), telemetryShutdownTimeout)
		defer cancel()
		if shutdownErr := tracing.Shutdown(shutdownContext); shutdownErr != nil {
			telemetryErr := fmt.Errorf("app: shutdown telemetry: %w", shutdownErr)
			if runErr == nil {
				runErr = telemetryErr
				return
			}
			runErr = errors.Join(runErr, telemetryErr)
		}
	}()

	databaseContext, cancel := context.WithTimeout(ctx, databaseStartupTimeout)
	defer cancel()
	pool, err := pgxpool.New(databaseContext, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("app: create database pool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(databaseContext); err != nil {
		return fmt.Errorf("app: database readiness: %w", err)
	}

	service := item.NewService(itempostgres.NewStore(pool))
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	var httpMetrics *metrics.Metrics
	if cfg.MetricsEnabled {
		httpMetrics = metrics.New()
	}
	handler := httpcontroller.NewRouter(
		service,
		pool.Ping,
		httpcontroller.WithMetrics(httpMetrics),
		httpcontroller.WithTracing(
			tracing.Tracer(httpcontroller.InstrumentationName),
			propagation.TraceContext{},
		),
	)
	server := httpserver.New(
		handler,
		httpserver.Port(cfg.HTTPPort),
		httpserver.ReadTimeout(10*time.Second),
		httpserver.WriteTimeout(15*time.Second),
		httpserver.ShutdownTimeout(10*time.Second),
	)
	server.Start()

	var serveErr error
	select {
	case <-ctx.Done():
		logger.Info("app - Run - context cancelled")
	case serverErr := <-server.Notify():
		if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
			serveErr = fmt.Errorf("app: HTTP server: %w", serverErr)
		}
	}

	if err := server.Shutdown(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("app: HTTP shutdown: %w", err)
	}
	return serveErr
}

// Package app configures and runs application.
package app

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/AJackTi/go-clean-architecture/config"
	http2 "github.com/AJackTi/go-clean-architecture/internal/controller/http"
	"github.com/AJackTi/go-clean-architecture/pkg/httpserver"
	"github.com/AJackTi/go-clean-architecture/pkg/logger"
	"github.com/AJackTi/go-clean-architecture/pkg/postgres"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// Run creates objects via constructors.
func Run(cfg *config.Config) {
	// Database
	pg, err := postgres.New(cfg)
	if err != nil {
		logger.Error("app - Run - postgres.New", logger.ErrWrap(err))
		return
	}
	defer pg.Close()

	// HTTP Server
	handler := gin.New()

	// middleware for all
	// cors allow all origins
	if *cfg.HTTP.Cors {
		logger.Info("Set CORS for testing, please don't use it in production")
		handler.Use(cors.Default())
	}

	http2.NewRouter(handler, cfg, pg)
	httpServer := httpserver.New(handler, httpserver.Port(cfg.HTTP.Port))

	// Waiting signal
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	select {
	case s := <-interrupt:
		logger.Info("app - Run - signal: " + s.String())
	case err = <-httpServer.Notify():
		logger.Error("app - Run - httpServer.Notify", logger.ErrWrap(err))
	}

	// Shutdown
	err = httpServer.Shutdown()
	if err != nil {
		logger.Error("app - Run - httpServer.Shutdown", logger.ErrWrap(err))
	}
}

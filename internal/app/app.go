// Package app configures and runs application.
package app

import (
	"net/http"
	"os"
	"os/signal"
	"syscall"

	http2 "github.com/AJackTi/go-clean-architecture/internal/controller/http"

	"github.com/AJackTi/go-clean-architecture/pkg/notification"

	"github.com/AJackTi/go-clean-architecture/pkg/aws"

	sseHandler "github.com/AJackTi/go-clean-architecture/pkg/sse"

	"github.com/AJackTi/go-clean-architecture/config"
	"github.com/AJackTi/go-clean-architecture/pkg/graph"
	"github.com/AJackTi/go-clean-architecture/pkg/httpserver"
	"github.com/AJackTi/go-clean-architecture/pkg/logger"
	"github.com/AJackTi/go-clean-architecture/pkg/postgres"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

const MaxChunkSize = 20
const TimeoutCheck = 30
const BatchSize = 200
const BatchSizeCities = 100
const BatchSizeDistricts = 1000
const BatchSizeWards = 15000
const TimeSleep = 10 // estimate time to confirm tx

type Condition struct {
	StartStage int    `json:"start_stage"`
	EndStage   int    `json:"end_stage"`
	OfficeCode string `json:"office_code"`
}

// Run creates objects via constructors.
func Run(cfg *config.Config) {
	// Database
	pg, err := postgres.New(cfg)
	if err != nil {
		logger.Error("app - Run - postgres.New", logger.ErrWrap(err))
	}
	defer pg.Close()

	client := http.Client{}
	graph := &graph.Graph{Client: &client}
	noti, err := notification.New()

	if err != nil {
		logger.Fatal("fail init notification firebase", logger.ErrWrap(err))
	}

	sseHandler := sseHandler.NewSSEHandler()

	s3, err := aws.New(cfg)
	if err != nil {
		logger.Fatal("fail init package s3 aws", logger.ErrWrap(err))
	}

	// HTTP Server
	handler := gin.New()

	// middleware for all
	// cors allow all origins
	if *cfg.HTTP.Cors {
		logger.Info("Set CORS for testing, please don't use it in production")
		handler.Use(cors.Default())
	}

	http2.NewRouter(handler, cfg, pg, graph, noti, sseHandler, s3)
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

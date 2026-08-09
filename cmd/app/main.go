package main

import (
	"log"

	"github.com/AJackTi/go-clean-architecture/config"
	"github.com/AJackTi/go-clean-architecture/internal/app"
	"github.com/AJackTi/go-clean-architecture/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load configuration: %v", err)
	}

	if err := logger.Init(cfg.LogLevel); err != nil {
		log.Fatalf("initialize logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()
	if err := app.Run(cfg); err != nil {
		log.Fatalf("run application: %v", err)
	}
}

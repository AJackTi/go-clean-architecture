package main

import (
	"log"

	"github.com/AJackTi/go-clean-architecture/config"
	"github.com/AJackTi/go-clean-architecture/internal/app"
	"github.com/AJackTi/go-clean-architecture/pkg/logger"
)

func main() {
	// Configuration
	cfg, err := config.NewConfig()
	if err != nil {
		log.Fatal("Config error", logger.ErrWrap(err))
	}

	// Logger
	logger.Init(cfg.Log.Level)

	// print log to confirm we load correct important values
	logger.Info("loaded config and init log ok")

	// Main web app run
	app.Run(cfg)
}

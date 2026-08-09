// Package v1 implements routing paths. Each services in own file.
package http

import (
	"net/http"

	"github.com/AJackTi/go-clean-architecture/internal/common"
	v1 "github.com/AJackTi/go-clean-architecture/internal/controller/http/v1"
	"github.com/AJackTi/go-clean-architecture/internal/entity"
	"github.com/gin-gonic/gin"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	// Swagger docs, must have in order to be able to display Swagger doc
	"github.com/AJackTi/go-clean-architecture/config"
	"github.com/AJackTi/go-clean-architecture/internal/usecase"
	"github.com/AJackTi/go-clean-architecture/pkg/postgres"
)

// NewRouter -.
// Swagger spec:
// @title       Golang Clean Architecture API
// @description Golang Clean Architecture
// @version     0.1.0
// @schemes     https
// @BasePath    /api/v1/
func NewRouter(handler *gin.Engine,
	cfg *config.Config,
	pg postgres.Postgres,
) {
	// Options
	handler.Use(gin.Logger())
	handler.Use(gin.Recovery())

	// Swagger
	if cfg.App.Env != "production" {
		swaggerHandler := ginSwagger.DisablingWrapHandler(swaggerFiles.Handler, "DISABLE_SWAGGER_HTTP_HANDLER")
		handler.GET("/swagger/*any", swaggerHandler)
	}

	// K8s probe - simple health check: just check application
	handler.GET("/api/health", func(c *gin.Context) { c.Status(http.StatusOK) })

	// complex health check: check all dependencies
	handler.GET("/api/healthz", func(c *gin.Context) {
		if err := pg.Ping(); err != nil {
			common.ErrorResponse(c, http.StatusInternalServerError, "cannot reach db", "cannot reach db")
			return
		}
		c.Status(http.StatusOK)
	})

	// Routers
	h := handler.Group("/api/v1")
	{
		// Use case
		itemUseCase := usecase.NewItemUseCase(entity.Items(pg))

		handlerController := v1.New(itemUseCase)

		handlerController.NewItemRoutes(h)
	}
}

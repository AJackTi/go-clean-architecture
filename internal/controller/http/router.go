// Package v1 implements routing paths. Each services in own file.
package http

import (
	"net/http"

	v1 "github.com/AJackTi/go-clean-architecture/internal/controller/http/v1"
	"github.com/AJackTi/go-clean-architecture/internal/entity"

	"github.com/AJackTi/go-clean-architecture/internal/common"

	"github.com/AJackTi/go-clean-architecture/pkg/notification"

	"github.com/AJackTi/go-clean-architecture/pkg/aws"

	sseHandler "github.com/AJackTi/go-clean-architecture/pkg/sse"

	"github.com/gin-gonic/gin"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	// Swagger docs, must have in order to be able to display Swagger doc
	"github.com/AJackTi/go-clean-architecture/config"
	"github.com/AJackTi/go-clean-architecture/internal/usecase"
	"github.com/AJackTi/go-clean-architecture/pkg/graph"
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
	graph *graph.Graph,
	notificationModel *notification.Notification,
	sseHandler *sseHandler.SSEHandler,
	s3 *aws.S3) {
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
		error := pg.Ping()
		if error != nil {
			common.ErrorResponse(c, http.StatusInternalServerError, "cannot reach db", "cannot reach db")
		}
		c.Status(http.StatusOK)
	})

	// Routers
	h := handler.Group("/api/v1")
	{
		// Use case
		itemUseCase := usecase.NewItemUseCase(entity.Items(pg))

		sseHandler.HandleEvents()

		handlerController := v1.New(
			itemUseCase,
			cfg,
			graph,
			notificationModel,
			sseHandler,
			s3)

		handlerController.NewItemRoutes(h)
	}
}

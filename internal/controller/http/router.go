// Package http composes the HTTP delivery adapters.
package http

import (
	"context"
	nethttp "net/http"

	"github.com/AJackTi/go-clean-architecture/internal/item/httpapi"
	"github.com/gin-gonic/gin"
)

const (
	livenessPath  = "/api/health"
	readinessPath = "/api/healthz"
)

// ReadinessCheck reports whether the application's required dependencies are
// available. The request context lets a failed client or server shutdown
// cancel an in-flight check.
type ReadinessCheck func(context.Context) error

// NewRouter wires middleware, operational endpoints, and the versioned API.
func NewRouter(items httpapi.Service, readiness ReadinessCheck) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	_ = router.SetTrustedProxies(nil)

	router.GET(livenessPath, func(c *gin.Context) {
		c.JSON(nethttp.StatusOK, healthResponse{Status: "ok"})
	})
	router.GET(readinessPath, func(c *gin.Context) {
		if readiness == nil || readiness(c.Request.Context()) != nil {
			c.JSON(nethttp.StatusServiceUnavailable, healthResponse{Status: "unavailable"})
			return
		}
		c.JSON(nethttp.StatusOK, healthResponse{Status: "ok"})
	})

	httpapi.New(items).RegisterRoutes(router.Group("/api/v1"))
	return router
}

type healthResponse struct {
	Status string `json:"status"`
}

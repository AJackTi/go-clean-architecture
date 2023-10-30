package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type itemRoutes struct {
	// define use case here
}

func (hand *handler) NewItemRoutes(handler *gin.RouterGroup) *handler {
	r := &itemRoutes{}

	h := handler.Group("/items")
	{
		h.POST("", r.CreateItem)
	}

	return hand
}

func (r *itemRoutes) CreateItem(c *gin.Context) {
	c.JSON(http.StatusOK, "OK")
}

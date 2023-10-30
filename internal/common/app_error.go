package common

import (
	"fmt"

	"github.com/AJackTi/go-clean-architecture/pkg/logger"
	"github.com/gin-gonic/gin"
)

type response struct {
	Error   string `json:"error" example:"message"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func ErrorResponse(c *gin.Context, code int, msg, err string) {
	logger.Error(fmt.Sprintf("error response: %d - %s", code, msg))
	c.AbortWithStatusJSON(code, response{Error: err, Code: code, Message: msg})
}

type Error struct {
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

func (e *Error) Is(target error) bool {
	return e.Message == target.Error()
}

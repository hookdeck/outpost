package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func AbortWithError(c *gin.Context, code int, err error) {
	c.Status(code)
	c.Error(err)
	c.Abort()
}

func AbortWithValidationError(c *gin.Context, err error) {
	errorResponse := ErrorResponse{}
	errorResponse.Parse(err)
	AbortWithError(c, http.StatusUnprocessableEntity, errorResponse)
}

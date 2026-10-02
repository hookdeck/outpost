package apirouter

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
	if isRequestBodyTooLarge(err) {
		AbortWithError(c, http.StatusRequestEntityTooLarge, NewErrRequestBodyTooLarge())
		return
	}
	errorResponse := ErrorResponse{}
	errorResponse.Parse(err)
	errorResponse.Code = http.StatusUnprocessableEntity
	AbortWithError(c, http.StatusUnprocessableEntity, errorResponse)
}

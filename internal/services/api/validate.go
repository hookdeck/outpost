package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
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

func (e *ErrorResponse) Parse(err error) {
	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		out := map[string]string{}
		for _, err := range validationErrors {
			out[err.Field()] = err.Tag()
		}
		e.Code = -1
		e.Message = "validation error"
		e.Data = out
		return
	}
	e.Message = err.Error()
}

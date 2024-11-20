package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

func BindJSON(c *gin.Context, obj interface{}) error {
	if err := c.ShouldBindJSON(obj); err != nil {
		c.Status(http.StatusUnprocessableEntity)
		c.Error(err)
		c.Abort()
		return err
	}
	return nil
}

func parseValidationError(validationErrors validator.ValidationErrors) ErrorResponse {
	out := map[string]string{}
	for _, err := range validationErrors {
		out[err.Field()] = err.Tag()
	}
	return ErrorResponse{
		Code:    -1,
		Message: "validation error",
		Data:    out,
	}
}

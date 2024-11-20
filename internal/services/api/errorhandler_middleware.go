package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

type ErrorResponse struct {
	Code    int         `json:"-"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func ErrorHandlerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		err := c.Errors.Last()
		if err == nil {
			return
		}

		if isInvalidJSON(err) {
			handleErrorResponse(c, ErrorResponse{
				Code:    http.StatusBadRequest,
				Message: "invalid JSON",
			})
			return
		}

		var validationErrors validator.ValidationErrors
		if errors.As(err.Err, &validationErrors) {
			handleErrorResponse(c, parseValidationError(validationErrors))
			return
		}
	}
}

func handleErrorResponse(c *gin.Context, response ErrorResponse) {
	c.JSON(response.Code, response)
}

func isInvalidJSON(err *gin.Error) bool {
	if errors.Is(err.Err, io.EOF) || errors.Is(err.Err, io.ErrUnexpectedEOF) {
		return true
	}

	var syntaxError *json.SyntaxError
	var unmarshalTypeError *json.UnmarshalTypeError
	return errors.As(err.Err, &syntaxError) || errors.As(err.Err, &unmarshalTypeError)
}

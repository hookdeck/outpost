package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type ErrorResponse struct {
	Err     error       `json:"-"`
	Code    int         `json:"-"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e ErrorResponse) Error() string {
	return e.Message
}

func NewErrInternalServer(err error) ErrorResponse {
	return ErrorResponse{
		Err:     err,
		Code:    http.StatusInternalServerError,
		Message: "internal server error",
	}
}

func ErrorHandlerMiddleware(logger *otelzap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		err := c.Errors.Last()
		if err == nil {
			return
		}

		var errorResponse ErrorResponse
		if errors.As(err.Err, &errorResponse) {
			if errorResponse.Code > 499 {
				logger.Ctx(c.Request.Context()).Error("internal server error", zap.Error(errorResponse.Err))
			}
			handleErrorResponse(c, errorResponse)
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
			errorResponse := ErrorResponse{}
			errorResponse.Parse(validationErrors)
			handleErrorResponse(c, errorResponse)
			return
		}
	}
}

func handleErrorResponse(c *gin.Context, response ErrorResponse) {
	c.JSON(response.Code, response)
}

func isInvalidJSON(err *gin.Error) bool {
	var syntaxError *json.SyntaxError
	var unmarshalTypeError *json.UnmarshalTypeError
	return errors.Is(err.Err, io.EOF) ||
		errors.Is(err.Err, io.ErrUnexpectedEOF) ||
		errors.As(err.Err, &syntaxError) ||
		errors.As(err.Err, &unmarshalTypeError)
}

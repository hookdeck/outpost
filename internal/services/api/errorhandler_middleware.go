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

func ErrorHandlerMiddleware(logger *otelzap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		err := c.Errors.Last()
		if err == nil {
			return
		}

		var errorResponse ErrorResponse
		errorResponse.Parse(err.Err)

		if errorResponse.Code > 499 {
			logger.Ctx(c.Request.Context()).Error("internal server error", zap.Error(errorResponse.Err))
		}
		handleErrorResponse(c, errorResponse)
	}
}

type ErrorResponse struct {
	Err     error       `json:"-"`
	Code    int         `json:"-"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (e ErrorResponse) Error() string {
	return e.Message
}

func (e *ErrorResponse) Parse(err error) {
	var errorResponse ErrorResponse
	if errors.As(err, &errorResponse) {
		*e = errorResponse
		return
	}

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
	if isInvalidJSON(err) {
		e.Code = http.StatusBadRequest
		e.Message = "invalid JSON"
		return
	}
	e.Message = err.Error()
}

func isInvalidJSON(err error) bool {
	var syntaxError *json.SyntaxError
	var unmarshalTypeError *json.UnmarshalTypeError
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.As(err, &syntaxError) ||
		errors.As(err, &unmarshalTypeError)
}

func handleErrorResponse(c *gin.Context, response ErrorResponse) {
	c.JSON(response.Code, response)
}

func NewErrInternalServer(err error) ErrorResponse {
	return ErrorResponse{
		Err:     err,
		Code:    http.StatusInternalServerError,
		Message: "internal server error",
	}
}

func NewErrBadRequest(err error) ErrorResponse {
	return ErrorResponse{
		Err:     err,
		Code:    http.StatusBadRequest,
		Message: err.Error(),
	}
}

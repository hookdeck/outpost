package apirouter

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/hookdeck/outpost/internal/destregistry"
	pkgerrors "github.com/pkg/errors"
)

func ErrorHandlerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		err := c.Errors.Last()
		if err == nil {
			return
		}

		var errorResponse ErrorResponse
		errorResponse.Parse(err.Err)
		handleErrorResponse(c, errorResponse)
	}
}

type ErrorResponse struct {
	Err     error       `json:"-"`
	Code    int         `json:"-"`
	Status  int         `json:"status"`
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
		e.Err = errorResponse.Err
		return
	}

	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		var messages []string
		for _, err := range validationErrors {
			messages = append(messages, formatValidationError(err.Field(), err.Tag(), err.Param()))
		}
		e.Code = http.StatusUnprocessableEntity
		e.Message = "validation error"
		e.Data = messages
		e.Err = err
		return
	}
	if isInvalidJSON(err) {
		e.Code = http.StatusBadRequest
		e.Message = "invalid JSON"
		e.Err = err
		return
	}

	// Handle destregistry.ErrDestinationValidation
	var validationErr *destregistry.ErrDestinationValidation
	if errors.As(err, &validationErr) {
		var messages []string
		for _, detail := range validationErr.Errors {
			messages = append(messages, formatValidationError(detail.Field, detail.Type, ""))
		}
		e.Code = http.StatusUnprocessableEntity
		e.Message = "validation error"
		e.Data = messages
		e.Err = err
		return
	}

	e.Message = err.Error()
	e.Err = err
}

// formatValidationError converts a validation error into a human-readable message.
// field is the field name, tag is the validation rule (e.g., "required", "min"),
// and param is the rule parameter (e.g., "6" for min=6).
func formatValidationError(field, tag, param string) string {
	// Convert field name to lowercase for consistency
	field = strings.ToLower(field)

	switch tag {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s characters", field, param)
	case "max":
		return fmt.Sprintf("%s must be at most %s characters", field, param)
	case "email":
		return fmt.Sprintf("%s must be a valid email address", field)
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, param)
	case "uuid":
		return fmt.Sprintf("%s must be a valid UUID", field)
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", field, param)
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", field, param)
	case "lt":
		return fmt.Sprintf("%s must be less than %s", field, param)
	case "lte":
		return fmt.Sprintf("%s must be less than or equal to %s", field, param)
	case "forbidden":
		return fmt.Sprintf("%s is forbidden", field)
	default:
		if param != "" {
			return fmt.Sprintf("%s failed %s=%s validation", field, tag, param)
		}
		return fmt.Sprintf("%s failed %s validation", field, tag)
	}
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
	response.Status = response.Code
	c.JSON(response.Code, response)
}

func NewErrInternalServer(err error) ErrorResponse {
	return ErrorResponse{
		Err:     pkgerrors.WithStack(err),
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

func NewErrNotFound(resource string) ErrorResponse {
	return ErrorResponse{
		Code:    http.StatusNotFound,
		Message: fmt.Sprintf("%s not found", resource),
	}
}

package apirouter

import (
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/hookdeck/outpost/internal/destregistry"
)

// Validation errors name fields by their JSON key. gin's validator is shared by the process, so this is set once.
func init() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterTagNameFunc(jsonFieldName)
	}
}

func jsonFieldName(fld reflect.StructField) string {
	name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
	if name == "-" {
		return ""
	}
	return name
}

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
	var rateLimited *destregistry.ErrDestinationRateLimited
	if errors.As(err, &rateLimited) {
		AbortWithError(c, http.StatusTooManyRequests, ErrorResponse{
			Code:       http.StatusTooManyRequests,
			Message:    "too many verification attempts",
			Data:       map[string]string{"code": rateLimited.Code},
			RetryAfter: rateLimited.RetryAfter,
		})
		return
	}
	var unavailable *destregistry.ErrDestinationUnavailable
	if errors.As(err, &unavailable) {
		AbortWithError(c, http.StatusServiceUnavailable, ErrorResponse{
			Code:    http.StatusServiceUnavailable,
			Message: "verification unavailable",
			Data:    map[string]string{"code": unavailable.Code},
		})
		return
	}
	errorResponse := ErrorResponse{}
	errorResponse.Parse(err)
	errorResponse.Code = http.StatusUnprocessableEntity
	AbortWithError(c, http.StatusUnprocessableEntity, errorResponse)
}

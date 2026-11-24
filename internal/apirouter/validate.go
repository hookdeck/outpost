package apirouter

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
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
	errorResponse := ErrorResponse{}
	errorResponse.Parse(err)
	errorResponse.Code = http.StatusUnprocessableEntity
	AbortWithError(c, http.StatusUnprocessableEntity, errorResponse)
}

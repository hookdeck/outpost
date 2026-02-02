package apirouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestErrorResponse_Parse_ValidationErrors(t *testing.T) {
	t.Parallel()

	type testInput struct {
		Email    string `validate:"required,email"`
		Password string `validate:"required,min=6"`
	}

	validate := validator.New()

	t.Run("produces array of human-readable messages for validator.ValidationErrors", func(t *testing.T) {
		t.Parallel()

		// Trigger validation errors
		input := testInput{Email: "", Password: ""}
		err := validate.Struct(input)
		require.Error(t, err)

		var errorResponse apirouter.ErrorResponse
		errorResponse.Parse(err)

		assert.Equal(t, http.StatusUnprocessableEntity, errorResponse.Code)
		assert.Equal(t, "validation error", errorResponse.Message)

		// Data should be []string
		messages, ok := errorResponse.Data.([]string)
		require.True(t, ok, "Data should be []string, got %T", errorResponse.Data)
		assert.Len(t, messages, 2)

		// Check that messages are human-readable (order may vary)
		assert.Contains(t, messages, "email is required")
		assert.Contains(t, messages, "password is required")
	})

	t.Run("includes validation param in message", func(t *testing.T) {
		t.Parallel()

		// Trigger min validation error with param
		input := testInput{Email: "test@example.com", Password: "abc"}
		err := validate.Struct(input)
		require.Error(t, err)

		var errorResponse apirouter.ErrorResponse
		errorResponse.Parse(err)

		messages, ok := errorResponse.Data.([]string)
		require.True(t, ok)
		assert.Contains(t, messages, "password must be at least 6 characters")
	})
}

func TestErrorResponse_Parse_DestRegistryValidation(t *testing.T) {
	t.Parallel()

	t.Run("produces array of human-readable messages for destregistry.ErrDestinationValidation", func(t *testing.T) {
		t.Parallel()

		err := destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
			{Field: "config.url", Type: "required"},
			{Field: "type", Type: "invalid_type"},
		})

		var errorResponse apirouter.ErrorResponse
		errorResponse.Parse(err)

		assert.Equal(t, http.StatusUnprocessableEntity, errorResponse.Code)
		assert.Equal(t, "validation error", errorResponse.Message)

		messages, ok := errorResponse.Data.([]string)
		require.True(t, ok, "Data should be []string, got %T", errorResponse.Data)
		assert.Len(t, messages, 2)
		assert.Contains(t, messages, "config.url is required")
		assert.Contains(t, messages, "type failed invalid_type validation")
	})
}

func TestHandleErrorResponse_SetsHandledAndStatus(t *testing.T) {
	t.Parallel()

	router := gin.New()
	router.Use(apirouter.ErrorHandlerMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.Error(apirouter.NewErrBadRequest(assert.AnError))
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, float64(http.StatusBadRequest), response["status"])
	assert.Equal(t, assert.AnError.Error(), response["message"])
}

func TestHandleErrorResponse_ValidationErrorFormat(t *testing.T) {
	t.Parallel()

	type requestBody struct {
		Name string `json:"name" binding:"required"`
	}

	router := gin.New()
	router.Use(apirouter.ErrorHandlerMiddleware())
	router.POST("/test", func(c *gin.Context) {
		var body requestBody
		if err := c.ShouldBindJSON(&body); err != nil {
			apirouter.AbortWithValidationError(c, err)
			return
		}
		c.JSON(http.StatusOK, body)
	})

	w := httptest.NewRecorder()
	// Use empty JSON object to trigger validation error (not JSON parse error)
	req, _ := http.NewRequest("POST", "/test", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, float64(http.StatusUnprocessableEntity), response["status"])
	assert.Equal(t, "validation error", response["message"])

	// Data should be an array
	data, ok := response["data"].([]interface{})
	require.True(t, ok, "data should be an array, got %T", response["data"])
	assert.Len(t, data, 1)
	assert.Equal(t, "name is required", data[0])
}

func TestErrorResponse_NotFoundFormat(t *testing.T) {
	t.Parallel()

	router := gin.New()
	router.Use(apirouter.ErrorHandlerMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.Error(apirouter.NewErrNotFound("tenant"))
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, float64(http.StatusNotFound), response["status"])
	assert.Equal(t, "tenant not found", response["message"])
}

func TestFormatValidationError_ForbiddenTag(t *testing.T) {
	t.Parallel()

	type testInput struct {
		Role string `validate:"forbidden"`
	}

	validate := validator.New()
	// Register a custom "forbidden" validator that always fails, so we can test the message.
	validate.RegisterValidation("forbidden", func(fl validator.FieldLevel) bool {
		return false
	})

	input := testInput{Role: "superadmin"}
	err := validate.Struct(input)
	require.Error(t, err)

	var errorResponse apirouter.ErrorResponse
	errorResponse.Parse(err)

	messages, ok := errorResponse.Data.([]string)
	require.True(t, ok, "Data should be []string, got %T", errorResponse.Data)
	assert.Contains(t, messages, "role is forbidden")
}

func TestErrorResponse_InternalServerErrorFormat(t *testing.T) {
	t.Parallel()

	router := gin.New()
	router.Use(apirouter.ErrorHandlerMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.Error(apirouter.NewErrInternalServer(assert.AnError))
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, float64(http.StatusInternalServerError), response["status"])
	assert.Equal(t, "internal server error", response["message"])
}

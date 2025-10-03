package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/services/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// mockMetadataLoader provides test metadata for different destination types
type mockMetadataLoader struct {
	metadata map[string]*metadata.ProviderMetadata
}

func (m *mockMetadataLoader) Load(providerType string) (*metadata.ProviderMetadata, error) {
	if meta, ok := m.metadata[providerType]; ok {
		return meta, nil
	}
	return nil, fmt.Errorf("metadata for provider %s not found", providerType)
}

// mockRegistry provides a test registry with sanitization capabilities
type mockRegistry struct {
	loader *mockMetadataLoader
}

func (r *mockRegistry) MetadataLoader() metadata.MetadataLoader {
	return r.loader
}

func (r *mockRegistry) ValidateDestination(ctx context.Context, destination *models.Destination) error {
	return nil
}

func (r *mockRegistry) PreprocessDestination(dest *models.Destination, orig *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	return nil
}

func (r *mockRegistry) DisplayDestination(dest *models.Destination) (*destregistry.DestinationDisplay, error) {
	// Create a copy of the destination with obfuscated credentials
	displayDest := *dest
	displayDest.Credentials = map[string]string{} // Credentials are obfuscated in display
	return &destregistry.DestinationDisplay{
		Destination: &displayDest,
	}, nil
}

func (r *mockRegistry) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *mockRegistry) PublishEvent(ctx context.Context, destination *models.Destination, event *models.Event) (*models.Delivery, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *mockRegistry) RegisterProvider(destinationType string, provider destregistry.Provider) error {
	return fmt.Errorf("not implemented")
}

func (r *mockRegistry) ResolveProvider(destination *models.Destination) (destregistry.Provider, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *mockRegistry) ResolvePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	return nil, fmt.Errorf("not implemented")
}

func (r *mockRegistry) RetrieveProviderMetadata(providerType string) (*metadata.ProviderMetadata, error) {
	return r.loader.Load(providerType)
}

func (r *mockRegistry) ListProviderMetadata() []*metadata.ProviderMetadata {
	var metadataList []*metadata.ProviderMetadata
	for _, meta := range r.loader.metadata {
		metadataList = append(metadataList, meta)
	}
	return metadataList
}

// setupTestEnvironment creates a test environment with logger middleware and sanitizer
func setupTestEnvironment(t *testing.T) (*gin.Engine, *observer.ObservedLogs, destregistry.Registry) {
	gin.SetMode(gin.TestMode)

	// Create observed logger to capture logs
	core, logs := observer.New(zap.InfoLevel)
	testLogger := &logging.Logger{Logger: otelzap.New(zap.New(core))}

	// Create mock metadata with sensitive fields
	metadataLoader := &mockMetadataLoader{
		metadata: map[string]*metadata.ProviderMetadata{
			"aws_kinesis": {
				Type: "aws_kinesis",
				ConfigFields: []metadata.FieldSchema{
					{Key: "stream_name", Sensitive: false, Required: true},
					{Key: "region", Sensitive: false, Required: true},
					{Key: "endpoint", Sensitive: false, Required: false},
				},
				CredentialFields: []metadata.FieldSchema{
					{Key: "access_key", Sensitive: true, Required: true},
					{Key: "secret_key", Sensitive: true, Required: true},
					{Key: "session_token", Sensitive: true, Required: false},
				},
			},
			"webhook": {
				Type: "webhook",
				ConfigFields: []metadata.FieldSchema{
					{Key: "url", Sensitive: false, Required: true},
					{Key: "method", Sensitive: false, Required: false},
				},
				CredentialFields: []metadata.FieldSchema{
					{Key: "api_key", Sensitive: true, Required: false},
					{Key: "bearer_token", Sensitive: true, Required: false},
				},
			},
		},
	}

	registry := &mockRegistry{loader: metadataLoader}

	// Create sanitizer and router
	sanitizer := api.NewRequestBodySanitizer(registry)
	router := gin.New()
	router.Use(api.LoggerMiddlewareWithSanitizer(testLogger, sanitizer))

	return router, logs, registry
}

// Test that 5xx errors properly log sanitized request bodies
func TestLoggerMiddleware_5xxErrorLogging(t *testing.T) {
	router, logs, _ := setupTestEnvironment(t)

	// Add a route that returns a 500 error
	router.POST("/api/v1/test/destinations", func(c *gin.Context) {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("database connection failed"))
	})

	// Test payload with sensitive credentials
	requestBody := map[string]interface{}{
		"type":   "aws_kinesis",
		"topics": []string{"test-topic"},
		"config": map[string]interface{}{
			"stream_name": "my-test-stream",
			"region":      "us-east-1",
		},
		"credentials": map[string]interface{}{
			"access_key":    "AKIAIOSFODNN7EXAMPLE",
			"secret_key":    "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			"session_token": "temporary-session-token",
		},
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	// Make request
	req := httptest.NewRequest("POST", "/api/v1/test/destinations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Verify response status
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Check that logs contain sanitized request body
	require.Greater(t, logs.Len(), 0, "Should have logged the error")

	var foundRequestBodyLog bool
	for _, logEntry := range logs.All() {
		for _, field := range logEntry.Context {
			if field.Key == "request_body" {
				foundRequestBodyLog = true
				requestBodyStr := field.String

				// Verify sensitive fields are redacted
				assert.Contains(t, requestBodyStr, "[REDACTED]", "Sensitive fields should be redacted")
				assert.NotContains(t, requestBodyStr, "AKIAIOSFODNN7EXAMPLE", "Access key should be redacted")
				assert.NotContains(t, requestBodyStr, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "Secret key should be redacted")
				assert.NotContains(t, requestBodyStr, "temporary-session-token", "Session token should be redacted")

				// Verify non-sensitive fields are preserved
				assert.Contains(t, requestBodyStr, "my-test-stream", "Non-sensitive config should be preserved")
				assert.Contains(t, requestBodyStr, "us-east-1", "Non-sensitive config should be preserved")
				assert.Contains(t, requestBodyStr, "aws_kinesis", "Destination type should be preserved")
			}
		}
	}

	assert.True(t, foundRequestBodyLog, "Should have logged request body for 5xx error")
}

// Test that 2xx responses don't log request bodies
func TestLoggerMiddleware_SuccessResponseNoBodyLogging(t *testing.T) {
	router, logs, _ := setupTestEnvironment(t)

	// Add a route that returns success
	router.POST("/api/v1/test/destinations", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"id": "test-123", "status": "created"})
	})

	requestBody := map[string]interface{}{
		"type": "aws_kinesis",
		"credentials": map[string]interface{}{
			"access_key": "AKIAIOSFODNN7EXAMPLE",
			"secret_key": "secret-value",
		},
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v1/test/destinations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Verify successful response
	assert.Equal(t, http.StatusCreated, w.Code)

	// Check that no request body is logged for successful requests
	for _, logEntry := range logs.All() {
		for _, field := range logEntry.Context {
			assert.NotEqual(t, "request_body", field.Key, "Should not log request body for successful requests")
		}
	}
}

// Test handling of oversized request bodies
func TestLoggerMiddleware_OversizedRequestBody(t *testing.T) {
	router, logs, _ := setupTestEnvironment(t)

	router.POST("/api/v1/test/destinations", func(c *gin.Context) {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("test error"))
	})

	// Create a request body larger than MaxRequestBodySize (10KB)
	largeData := strings.Repeat("a", 11*1024) // 11KB
	requestBody := map[string]interface{}{
		"type": "aws_kinesis",
		"data": largeData,
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v1/test/destinations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Verify error response
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Check that oversized body is handled with appropriate message
	var foundTruncatedLog bool
	for _, logEntry := range logs.All() {
		for _, field := range logEntry.Context {
			if field.Key == "request_body" {
				foundTruncatedLog = true
				assert.Contains(t, field.String, "[REQUEST_BODY_TOO_LARGE:", "Should indicate body is too large")
			}
		}
	}

	assert.True(t, foundTruncatedLog, "Should have logged truncated body message")
}

// Test handling of non-JSON request bodies
func TestLoggerMiddleware_NonJSONRequestBody(t *testing.T) {
	router, logs, _ := setupTestEnvironment(t)

	router.POST("/api/v1/test/destinations", func(c *gin.Context) {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("test error"))
	})

	// Send plain text body
	plainTextBody := "this is not json data"
	req := httptest.NewRequest("POST", "/api/v1/test/destinations", strings.NewReader(plainTextBody))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Check that non-JSON body is logged as-is
	var foundPlainTextLog bool
	for _, logEntry := range logs.All() {
		for _, field := range logEntry.Context {
			if field.Key == "request_body" {
				foundPlainTextLog = true
				assert.Equal(t, plainTextBody, field.String, "Non-JSON body should be logged as-is")
			}
		}
	}

	assert.True(t, foundPlainTextLog, "Should have logged non-JSON body")
}

// Test handling of empty request bodies
func TestLoggerMiddleware_EmptyRequestBody(t *testing.T) {
	router, logs, _ := setupTestEnvironment(t)

	router.POST("/api/v1/test/destinations", func(c *gin.Context) {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("test error"))
	})

	req := httptest.NewRequest("POST", "/api/v1/test/destinations", bytes.NewReader([]byte{}))
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Check that empty body is logged with [EMPTY_BODY] marker
	var foundEmptyBodyLog bool
	for _, logEntry := range logs.All() {
		for _, field := range logEntry.Context {
			if field.Key == "request_body" {
				foundEmptyBodyLog = true
				assert.Equal(t, "[EMPTY_BODY]", field.String, "Empty body should be logged as [EMPTY_BODY]")
			}
		}
	}

	assert.True(t, foundEmptyBodyLog, "Should have logged empty body marker")
}

// Test handling of nil request bodies
func TestLoggerMiddleware_NilRequestBody(t *testing.T) {
	router, logs, _ := setupTestEnvironment(t)

	router.POST("/api/v1/test/destinations", func(c *gin.Context) {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("test error"))
	})

	// Create a request with nil body (similar to what some existing tests do)
	req := httptest.NewRequest("POST", "/api/v1/test/destinations", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Check that nil body is treated as empty body and logged with [EMPTY_BODY] marker
	// Note: When using httptest.NewRequest(method, path, nil), Gin creates an empty body reader
	var foundEmptyBodyLog bool
	for _, logEntry := range logs.All() {
		for _, field := range logEntry.Context {
			if field.Key == "request_body" {
				foundEmptyBodyLog = true
				assert.Equal(t, "[EMPTY_BODY]", field.String, "Nil body should be treated as empty and logged as [EMPTY_BODY]")
			}
		}
	}

	assert.True(t, foundEmptyBodyLog, "Should have logged empty body marker for nil body")
}

// Test that publish endpoints are excluded from body buffering
func TestLoggerMiddleware_PublishEndpointExcluded(t *testing.T) {
	router, logs, _ := setupTestEnvironment(t)

	router.POST("/api/v1/publish", func(c *gin.Context) {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("test error"))
	})

	sensitiveBody := map[string]interface{}{
		"data": "sensitive user data that should not be logged",
	}

	bodyBytes, err := json.Marshal(sensitiveBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v1/publish", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Check that publish endpoint body is NOT logged even for 5xx errors
	for _, logEntry := range logs.All() {
		for _, field := range logEntry.Context {
			assert.NotEqual(t, "request_body", field.Key, "Publish endpoint bodies should not be logged")
		}
	}
}

// Test GET requests don't trigger body buffering
func TestLoggerMiddleware_GETRequestNoBuffering(t *testing.T) {
	router, logs, _ := setupTestEnvironment(t)

	router.GET("/api/v1/test/destinations", func(c *gin.Context) {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("test error"))
	})

	req := httptest.NewRequest("GET", "/api/v1/test/destinations", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Check that GET request doesn't log body (even though there isn't one)
	for _, logEntry := range logs.All() {
		for _, field := range logEntry.Context {
			assert.NotEqual(t, "request_body", field.Key, "GET requests should not trigger body logging")
		}
	}
}

// Test different destination types with various credential patterns
func TestLoggerMiddleware_DifferentDestinationTypes(t *testing.T) {
	router, logs, _ := setupTestEnvironment(t)

	router.POST("/api/v1/test/destinations", func(c *gin.Context) {
		c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("test error"))
	})

	tests := []struct {
		name         string
		requestBody  map[string]interface{}
		expectedMask []string // Fields that should be masked
		expectedKeep []string // Fields that should be preserved
	}{
		{
			name: "webhook destination",
			requestBody: map[string]interface{}{
				"type": "webhook",
				"config": map[string]interface{}{
					"url":    "https://api.example.com/webhook",
					"method": "POST",
				},
				"credentials": map[string]interface{}{
					"api_key":      "secret-api-key-123",
					"bearer_token": "bearer-token-456",
				},
			},
			expectedMask: []string{"secret-api-key-123", "bearer-token-456"},
			expectedKeep: []string{"https://api.example.com/webhook", "POST", "webhook"},
		},
		{
			name: "destination without credentials",
			requestBody: map[string]interface{}{
				"type": "webhook",
				"config": map[string]interface{}{
					"url":    "https://public-api.example.com/webhook",
					"method": "POST",
				},
			},
			expectedMask: []string{},
			expectedKeep: []string{"https://public-api.example.com/webhook", "POST", "webhook"},
		},
		{
			name: "unknown destination type",
			requestBody: map[string]interface{}{
				"type": "unknown_type",
				"credentials": map[string]interface{}{
					"some_field": "some_value",
				},
			},
			expectedMask: []string{},
			expectedKeep: []string{"unknown_type", "some_value"}, // Unknown type won't be sanitized
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear previous logs
			logs.TakeAll()

			bodyBytes, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/api/v1/test/destinations", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)

			// Find the request body log
			var requestBodyLog string
			for _, logEntry := range logs.All() {
				for _, field := range logEntry.Context {
					if field.Key == "request_body" {
						requestBodyLog = field.String
						break
					}
				}
			}

			if len(tt.expectedMask) > 0 || len(tt.expectedKeep) > 0 {
				require.NotEmpty(t, requestBodyLog, "Should have logged request body")

				// Check that sensitive fields are masked
				for _, sensitiveValue := range tt.expectedMask {
					assert.NotContains(t, requestBodyLog, sensitiveValue,
						"Sensitive value %s should be masked", sensitiveValue)
				}

				// Check that [REDACTED] appears for sensitive fields
				if len(tt.expectedMask) > 0 {
					assert.Contains(t, requestBodyLog, "[REDACTED]",
						"Should contain redaction marker for sensitive fields")
				}

				// Check that non-sensitive fields are preserved
				for _, keepValue := range tt.expectedKeep {
					assert.Contains(t, requestBodyLog, keepValue,
						"Non-sensitive value %s should be preserved", keepValue)
				}
			}
		})
	}
}

// Test that 4xx errors don't log request bodies
func TestLoggerMiddleware_4xxErrorNoLogging(t *testing.T) {
	router, logs, _ := setupTestEnvironment(t)

	router.POST("/api/v1/test/destinations", func(c *gin.Context) {
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("validation failed"))
	})

	requestBody := map[string]interface{}{
		"type": "aws_kinesis",
		"credentials": map[string]interface{}{
			"access_key": "should-not-be-logged",
		},
	}

	bodyBytes, err := json.Marshal(requestBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/api/v1/test/destinations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Check that 4xx errors don't log request body
	for _, logEntry := range logs.All() {
		for _, field := range logEntry.Context {
			assert.NotEqual(t, "request_body", field.Key, "4xx errors should not log request body")
		}
	}
}

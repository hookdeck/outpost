package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestMiddlewareOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Track middleware execution order
	var executionOrder []string
	var metricsLatency time.Duration
	var loggerLatency time.Duration

	// Mock metrics middleware
	mockMetrics := func() gin.HandlerFunc {
		return func(c *gin.Context) {
			executionOrder = append(executionOrder, "metrics_start")
			c.Next()
			executionOrder = append(executionOrder, "metrics_end")
			metricsLatency = GetRequestLatency(c)
		}
	}

	// Mock logger middleware
	mockLogger := func() gin.HandlerFunc {
		return func(c *gin.Context) {
			executionOrder = append(executionOrder, "logger_start")
			c.Next()
			executionOrder = append(executionOrder, "logger_end")
			loggerLatency = GetRequestLatency(c)
		}
	}

	// Create router with our middleware order
	r := gin.New()
	r.Use(mockMetrics())
	r.Use(mockLogger())
	r.Use(LatencyMiddleware())

	// Add a handler that sleeps to simulate work
	r.GET("/test", func(c *gin.Context) {
		executionOrder = append(executionOrder, "handler")
		time.Sleep(10 * time.Millisecond) // Simulate some work
	})

	// Make request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)

	// Verify execution order
	expectedOrder := []string{
		"metrics_start",
		"logger_start",
		"handler",
		"logger_end",
		"metrics_end",
	}
	assert.Equal(t, expectedOrder, executionOrder)

	// Verify latency was captured
	assert.Greater(t, metricsLatency, time.Duration(0))
	assert.Greater(t, loggerLatency, time.Duration(0))

	// Both middleware should see the same latency
	assert.Equal(t, metricsLatency, loggerLatency)

	// Latency should be at least our sleep duration
	assert.GreaterOrEqual(t, metricsLatency, 10*time.Millisecond)
}

package e2e_test

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/hookdeck/outpost/cmd/e2e/httpclient"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/stretchr/testify/require"
)

func (suite *basicSuite) TestHealthzAPI() {
	tests := []APITest{
		{
			Name: "GET /healthz",
			Request: httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/healthz",
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"status": map[string]interface{}{
							"type": "string",
						},
						"timestamp": map[string]interface{}{
							"type": "string",
						},
						"workers": map[string]interface{}{
							"type": "object",
						},
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestTenantsAPI() {
	tenantID := idgen.String()
	sampleDestinationID := idgen.Destination()
	tests := []APITest{
		{
			Name: "GET /:tenantID without auth header",
			Request: httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},
		{
			Name: "GET /:tenantID without tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "PUT /:tenantID without auth header",
			Request: httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},
		{
			Name: "PUT /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
					},
				},
			},
		},
		{
			Name: "GET /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
					},
				},
			},
		},
		{
			Name: "PUT /:tenantID again",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     sampleDestinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "GET /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 1,
						"topics":             suite.config.Topics,
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
				Body: map[string]interface{}{
					"topics": []string{suite.config.Topics[0]},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 1,
						"topics":             []string{suite.config.Topics[0]},
					},
				},
			},
		},
		{
			Name: "DELETE /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
					},
				},
			},
		},
		{
			Name: "DELETE /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "PUT /:tenantID should override deleted tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
					},
				},
			},
		},
		// Metadata tests
		{
			Name: "PUT /:tenantID with metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
				Body: map[string]interface{}{
					"metadata": map[string]interface{}{
						"environment": "production",
						"team":        "platform",
						"region":      "us-east-1",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id": tenantID,
						"metadata": map[string]interface{}{
							"environment": "production",
							"team":        "platform",
							"region":      "us-east-1",
						},
					},
				},
			},
		},
		{
			Name: "GET /:tenantID retrieves metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id": tenantID,
						"metadata": map[string]interface{}{
							"environment": "production",
							"team":        "platform",
							"region":      "us-east-1",
						},
					},
				},
			},
		},
		{
			Name: "PUT /:tenantID replaces metadata (full replacement)",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
				Body: map[string]interface{}{
					"metadata": map[string]interface{}{
						"team":  "engineering",
						"owner": "alice",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id": tenantID,
						"metadata": map[string]interface{}{
							"team":  "engineering",
							"owner": "alice",
							// Note: environment and region are gone (full replacement)
						},
					},
				},
			},
		},
		{
			Name: "GET /:tenantID verifies metadata was replaced",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id": tenantID,
						"metadata": map[string]interface{}{
							"team":  "engineering",
							"owner": "alice",
						},
					},
				},
			},
		},
		{
			Name: "PUT /:tenantID without metadata clears it",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
				Body:   map[string]interface{}{},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID verifies metadata is nil",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":                 tenantID,
						"destinations_count": 0,
						"topics":             []string{},
						// metadata field should not be present (omitempty)
					},
				},
			},
		},
		{
			Name: "Create new tenant with metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + idgen.String(),
				Body: map[string]interface{}{
					"metadata": map[string]interface{}{
						"stage": "development",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
					Body: map[string]interface{}{
						"metadata": map[string]interface{}{
							"stage": "development",
						},
					},
				},
			},
		},
		{
			Name: "PUT /:tenantID with metadata value auto-converted (number to string)",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + idgen.String(),
				Body: map[string]interface{}{
					"metadata": map[string]interface{}{
						"count":   42,
						"enabled": true,
						"ratio":   3.14,
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
					Body: map[string]interface{}{
						"metadata": map[string]interface{}{
							"count":   "42",
							"enabled": "true",
							"ratio":   "3.14",
						},
					},
				},
			},
		},
		{
			Name: "PUT /:tenantID with empty body (no metadata)",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + idgen.String(),
				Body:   map[string]interface{}{},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestTenantAPIInvalidJSON() {
	t := suite.T()
	tenantID := idgen.String()
	baseURL := fmt.Sprintf("http://localhost:%d/api/v1", suite.config.APIPort)

	// Create tenant with malformed JSON (send raw bytes)
	jsonBody := []byte(`{"metadata": invalid json}`)
	req, err := http.NewRequest(httpclient.MethodPUT, baseURL+"/"+tenantID, bytes.NewReader(jsonBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+suite.config.APIKey)

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "Malformed JSON should return 400")
}

func (suite *basicSuite) TestDestinationsAPI() {
	tenantID := idgen.String()
	sampleDestinationID := idgen.Destination()
	destinationWithMetadataID := idgen.Destination()
	tests := []APITest{
		{
			Name: "PUT /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
		{
			Name: "POST /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with no body JSON",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusBadRequest,
					Body: map[string]interface{}{
						"message": "invalid JSON",
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with empty body JSON",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body:   map[string]interface{}{},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": map[string]interface{}{
							"topics": "required",
							"type":   "required",
						},
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with invalid topics",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": "invalid",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation failed: invalid topics format",
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with invalid topics",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": []string{"invalid"},
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation failed: invalid topics",
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with invalid config",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": []string{"user.created"},
					"config": map[string]interface{}{},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": map[string]interface{}{
							"config.url": "required",
						},
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with user-provided ID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     sampleDestinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with delivery_metadata and metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationWithMetadataID,
					"type":   "webhook",
					"topics": []string{"user.created"},
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
					"delivery_metadata": map[string]interface{}{
						"X-App-ID":  "test-app",
						"X-Version": "1.0",
					},
					"metadata": map[string]interface{}{
						"environment": "test",
						"team":        "platform",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID with delivery_metadata and metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationWithMetadataID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     destinationWithMetadataID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
						"delivery_metadata": map[string]interface{}{
							"X-App-ID":  "test-app",
							"X-Version": "1.0",
						},
						"metadata": map[string]interface{}{
							"environment": "test",
							"team":        "platform",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID update delivery_metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationWithMetadataID,
				Body: map[string]interface{}{
					"delivery_metadata": map[string]interface{}{
						"X-Version": "2.0",       // Overwrite existing value (was "1.0")
						"X-Region":  "us-east-1", // Add new key
					},
					// Note: X-App-ID not included, should be preserved from original
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     destinationWithMetadataID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
						"delivery_metadata": map[string]interface{}{
							"X-App-ID":  "test-app",  // PRESERVED: Not in PATCH request
							"X-Version": "2.0",       // OVERWRITTEN: Updated from "1.0"
							"X-Region":  "us-east-1", // NEW: Added by PATCH request
						},
						"metadata": map[string]interface{}{
							"environment": "test",
							"team":        "platform",
						},
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID update metadata",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + destinationWithMetadataID,
				Body: map[string]interface{}{
					"metadata": map[string]interface{}{
						"team":   "engineering", // Overwrite existing value (was "platform")
						"region": "us",          // Add new key
					},
					// Note: environment not included, should be preserved from original
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     destinationWithMetadataID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
						"delivery_metadata": map[string]interface{}{
							"X-App-ID":  "test-app",
							"X-Version": "2.0",
							"X-Region":  "us-east-1",
						},
						"metadata": map[string]interface{}{
							"environment": "test",        // PRESERVED: Not in PATCH request
							"team":        "engineering", // OVERWRITTEN: Updated from "platform"
							"region":      "us",          // NEW: Added by PATCH request
						},
					},
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID verify merged fields",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationWithMetadataID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     destinationWithMetadataID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
						// Verify delivery_metadata merge behavior persists:
						// - Original: {"X-App-ID": "test-app", "X-Version": "1.0"}
						// - After PATCH 1: {"X-Version": "2.0", "X-Region": "us-east-1"}
						// - Result: Preserved X-App-ID, overwrote X-Version, added X-Region
						"delivery_metadata": map[string]interface{}{
							"X-App-ID":  "test-app",
							"X-Version": "2.0",
							"X-Region":  "us-east-1",
						},
						// Verify metadata merge behavior persists:
						// - Original: {"environment": "test", "team": "platform"}
						// - After PATCH 2: {"team": "engineering", "region": "us"}
						// - Result: Preserved environment, overwrote team, added region
						"metadata": map[string]interface{}{
							"environment": "test",
							"team":        "engineering",
							"region":      "us",
						},
					},
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations with duplicate ID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     sampleDestinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusBadRequest,
					Body: map[string]interface{}{
						"message": "destination already exists",
					},
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(3),
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     sampleDestinationID,
						"type":   "webhook",
						"topics": []string{"*"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
				Body: map[string]interface{}{
					"topics": []string{"user.created"},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     sampleDestinationID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
					},
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: map[string]interface{}{
						"id":     sampleDestinationID,
						"type":   "webhook",
						"topics": []string{"user.created"},
						"config": map[string]interface{}{
							"url": "http://host.docker.internal:4444",
						},
						"credentials": map[string]interface{}{},
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
				Body: map[string]interface{}{
					"topics": []string{""},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation failed: invalid topics",
					},
				},
			},
		},
		{
			Name: "PATCH /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPATCH,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
				Body: map[string]interface{}{
					"config": map[string]interface{}{
						"url": "",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnprocessableEntity,
					Body: map[string]interface{}{
						"message": "validation error",
						"data": map[string]interface{}{
							"config.url": "required",
						},
					},
				},
			},
		},
		{
			Name: "DELETE /:tenantID/destinations/:destinationID with invalid destination ID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/" + tenantID + "/destinations/" + idgen.Destination(),
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "DELETE /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(2),
			},
		},
		{
			Name: "POST /:tenantID/destinations with metadata auto-conversion",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
					"metadata": map[string]interface{}{
						"priority": 10,
						"enabled":  true,
						"version":  1.5,
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
					Body: map[string]interface{}{
						"metadata": map[string]interface{}{
							"priority": "10",
							"enabled":  "true",
							"version":  "1.5",
						},
					},
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestEntityUpdatedAt() {
	t := suite.T()
	tenantID := idgen.String()
	destinationID := idgen.Destination()

	// Create tenant
	resp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPUT,
		Path:   "/" + tenantID,
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Get tenant and verify created_at and updated_at exist and are equal
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/" + tenantID,
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body := resp.Body.(map[string]interface{})
	require.NotNil(t, body["created_at"], "created_at should be present")
	require.NotNil(t, body["updated_at"], "updated_at should be present")

	tenantCreatedAt := body["created_at"].(string)
	tenantUpdatedAt := body["updated_at"].(string)
	// On creation, created_at and updated_at should be very close (within 1 second)
	createdTime, err := time.Parse(time.RFC3339Nano, tenantCreatedAt)
	require.NoError(t, err)
	updatedTime, err := time.Parse(time.RFC3339Nano, tenantUpdatedAt)
	require.NoError(t, err)
	require.WithinDuration(t, createdTime, updatedTime, time.Second, "created_at and updated_at should be close on creation")

	// Wait a bit to ensure different timestamp
	time.Sleep(10 * time.Millisecond)

	// Update tenant
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPUT,
		Path:   "/" + tenantID,
		Body: map[string]interface{}{
			"metadata": map[string]interface{}{
				"env": "production",
			},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Get tenant again and verify updated_at changed but created_at didn't
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/" + tenantID,
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body = resp.Body.(map[string]interface{})
	newTenantCreatedAt := body["created_at"].(string)
	newTenantUpdatedAt := body["updated_at"].(string)

	require.Equal(t, tenantCreatedAt, newTenantCreatedAt, "created_at should not change")
	require.NotEqual(t, tenantUpdatedAt, newTenantUpdatedAt, "updated_at should change")
	require.True(t, newTenantUpdatedAt > tenantUpdatedAt, "updated_at should be newer")

	// Create destination
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPOST,
		Path:   "/" + tenantID + "/destinations",
		Body: map[string]interface{}{
			"id":     destinationID,
			"type":   "webhook",
			"topics": []string{"*"},
			"config": map[string]interface{}{
				"url": "http://host.docker.internal:4444",
			},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	// Get destination and verify created_at and updated_at exist and are equal
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/" + tenantID + "/destinations/" + destinationID,
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body = resp.Body.(map[string]interface{})
	require.NotNil(t, body["created_at"], "created_at should be present")
	require.NotNil(t, body["updated_at"], "updated_at should be present")

	destCreatedAt := body["created_at"].(string)
	destUpdatedAt := body["updated_at"].(string)
	// On creation, created_at and updated_at should be very close (within 1 second)
	createdTime, err = time.Parse(time.RFC3339Nano, destCreatedAt)
	require.NoError(t, err)
	updatedTime, err = time.Parse(time.RFC3339Nano, destUpdatedAt)
	require.NoError(t, err)
	require.WithinDuration(t, createdTime, updatedTime, time.Second, "created_at and updated_at should be close on creation")

	// Wait a bit to ensure different timestamp
	time.Sleep(10 * time.Millisecond)

	// Update destination
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodPATCH,
		Path:   "/" + tenantID + "/destinations/" + destinationID,
		Body: map[string]interface{}{
			"topics": []string{"user.created"},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Get destination again and verify updated_at changed but created_at didn't
	resp, err = suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/" + tenantID + "/destinations/" + destinationID,
	}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body = resp.Body.(map[string]interface{})
	newDestCreatedAt := body["created_at"].(string)
	newDestUpdatedAt := body["updated_at"].(string)

	require.Equal(t, destCreatedAt, newDestCreatedAt, "created_at should not change")
	require.NotEqual(t, destUpdatedAt, newDestUpdatedAt, "updated_at should change")
	require.True(t, newDestUpdatedAt > destUpdatedAt, "updated_at should be newer")
}

func (suite *basicSuite) TestDestinationsListAPI() {
	tenantID := idgen.String()
	tests := []APITest{
		{
			Name: "PUT /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations type=webhook topics=*",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations type=webhook topics=user.created",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": []string{"user.created"},
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations type=webhook topics=user.created user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": []string{"user.created", "user.updated"},
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(3),
			},
		},
		{
			Name: "GET /:tenantID/destinations?type=webhook",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?type=webhook",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(3),
			},
		},
		{
			Name: "GET /:tenantID/destinations?type=rabbitmq",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?type=rabbitmq",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
		{
			Name: "GET /:tenantID/destinations?topics=*",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?topics=*",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(1),
			},
		},
		{
			Name: "GET /:tenantID/destinations?topics=user.created",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?topics=user.created",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(3),
			},
		},
		{
			Name: "GET /:tenantID/destinations?topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(2),
			},
		},
		{
			Name: "GET /:tenantID/destinations?topics=user.created&topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?topics=user.created&topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(2),
			},
		},
		{
			Name: "GET /:tenantID/destinations?type=webhook&topics=user.created&topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?type=webhook&topics=user.created&topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(2),
			},
		},
		{
			Name: "GET /:tenantID/destinations?type=rabbitmq&topics=user.created&topics=user.updated",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations?type=rabbitmq&topics=user.created&topics=user.updated",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestDestinationEnableDisableAPI() {
	tenantID := idgen.String()
	sampleDestinationID := idgen.Destination()
	tests := []APITest{
		{
			Name: "PUT /:tenantID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "POST /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     sampleDestinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "PUT /:tenantID/destinations/:destinationID/disable",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID + "/disable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "PUT /:tenantID/destinations/:destinationID/enable",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID + "/enable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "PUT /:tenantID/destinations/:destinationID/enable duplicate",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID + "/enable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, false),
			},
		},
		{
			Name: "PUT /:tenantID/destinations/:destinationID/disable",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID + "/disable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "PUT /:tenantID/destinations/:destinationID/disable duplicate",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID + "/disable",
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
		{
			Name: "GET /:tenantID/destinations/:destinationID",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + sampleDestinationID,
			}),
			Expected: APITestExpectation{
				Validate: makeDestinationDisabledValidator(sampleDestinationID, true),
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestTopicsAPI() {
	tests := []APITest{
		{
			Name: "GET /topics",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/topics",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
					Body:       suite.config.Topics,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestDestinationTypesAPI() {
	providerFieldSchema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"key", "type", "label", "description", "required"},
		"properties": map[string]interface{}{
			"key":         map[string]interface{}{"type": "string"},
			"type":        map[string]interface{}{"type": "string"},
			"label":       map[string]interface{}{"type": "string"},
			"description": map[string]interface{}{"type": "string"},
			"required":    map[string]interface{}{"type": "boolean"},
		},
	}

	providerSchema := map[string]interface{}{
		"type":     "object",
		"required": []interface{}{"type", "label", "description", "icon", "config_fields", "credential_fields"},
		"properties": map[string]interface{}{
			"type":         map[string]interface{}{"type": "string"},
			"label":        map[string]interface{}{"type": "string"},
			"description":  map[string]interface{}{"type": "string"},
			"icon":         map[string]interface{}{"type": "string"},
			"instructions": map[string]interface{}{"type": "string"},
			"config_fields": map[string]interface{}{
				"type":  "array",
				"items": providerFieldSchema,
			},
			"credential_fields": map[string]interface{}{
				"type":  "array",
				"items": providerFieldSchema,
			},
			"validation": map[string]interface{}{
				"type": "object",
			},
		},
	}

	tests := []APITest{
		{
			Name: "GET /destination-types",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/destination-types",
			}),
			Expected: APITestExpectation{
				Validate: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"statusCode": map[string]any{"const": 200},
						"body": map[string]interface{}{
							"type":        "array",
							"items":       providerSchema,
							"minItems":    8,
							"maxItems":    8,
							"uniqueItems": true,
						},
					},
				},
			},
		},
		{
			Name: "GET /destination-types/webhook",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/destination-types/webhook",
			}),
			Expected: APITestExpectation{
				Validate: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"statusCode": map[string]any{"const": 200},
						"body":       providerSchema,
					},
				},
			},
		},
		{
			Name: "GET /destination-types/invalid",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/destination-types/invalid",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusNotFound,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), tests)
}

func (suite *basicSuite) TestJWTAuthAPI() {
	// Step 1: Create tenant and get JWT token
	tenantID := idgen.String()
	destinationID := idgen.Destination()

	// Create tenant first using admin auth
	createTenantTests := []APITest{
		{
			Name: "PUT /:tenantID to create tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
	}
	suite.RunAPITests(suite.T(), createTenantTests)

	// Step 2: Get JWT token - need to do this manually since we need to extract the token
	tokenResp, err := suite.client.Do(suite.AuthRequest(httpclient.Request{
		Method: httpclient.MethodGET,
		Path:   "/" + tenantID + "/token",
	}))
	suite.Require().NoError(err)
	suite.Require().Equal(http.StatusOK, tokenResp.StatusCode)

	bodyMap := tokenResp.Body.(map[string]interface{})
	token := bodyMap["token"].(string)
	suite.Require().NotEmpty(token)

	// Step 3: Test various endpoints with JWT auth
	jwtTests := []APITest{
		// Test tenant-specific routes with tenantID param
		{
			Name: "GET /:tenantID with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID,
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID/destinations with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations",
			}, token),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(0),
			},
		},
		{
			Name: "POST /:tenantID/destinations with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},

		// Test tenant-specific routes without tenantID param
		{
			Name: "GET /destinations with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/destinations",
			}, token),
			Expected: APITestExpectation{
				Validate: makeDestinationListValidator(1),
			},
		},
		{
			Name: "POST /destinations with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/destinations",
				Body: map[string]interface{}{
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": "http://host.docker.internal:4444",
					},
				},
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},

		// Test tenant-agnostic routes with tenantID param
		{
			Name: "GET /:tenantID/destination-types with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destination-types",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /:tenantID/topics with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/topics",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},

		// Test tenant-agnostic routes without tenantID param
		{
			Name: "GET /destination-types with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/destination-types",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		{
			Name: "GET /topics with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/topics",
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},

		// Test wrong tenantID
		{
			Name: "GET /wrong-tenant-id with JWT should fail",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + idgen.String(),
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusUnauthorized,
				},
			},
		},

		// Clean up - delete tenant
		{
			Name: "DELETE /:tenantID with JWT should work",
			Request: suite.AuthJWTRequest(httpclient.Request{
				Method: httpclient.MethodDELETE,
				Path:   "/" + tenantID,
			}, token),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
	}

	suite.RunAPITests(suite.T(), jwtTests)
}

func makeDestinationListValidator(length int) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"statusCode": map[string]any{
				"const": 200,
			},
			"body": map[string]any{
				"type":     "array",
				"minItems": length,
				"maxItems": length,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type": "string",
						},
						"type": map[string]any{
							"type": "string",
						},
						"config": map[string]any{
							"type": "object",
						},
						"credentials": map[string]any{
							"type": "object",
						},
					},
					"required": []any{"id", "type", "config", "credentials"},
				},
			},
		},
	}
}

func makeDestinationDisabledValidator(id string, disabled bool) map[string]any {
	var disabledValidator map[string]any
	if disabled {
		disabledValidator = map[string]any{
			"type":      "string",
			"minLength": 1,
		}
	} else {
		disabledValidator = map[string]any{
			"type": "null",
		}
	}
	return map[string]interface{}{
		"properties": map[string]interface{}{
			"statusCode": map[string]interface{}{
				"const": 200,
			},
			"body": map[string]interface{}{
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"const": id,
					},
					"disabled_at": disabledValidator,
				},
			},
		},
	}
}

func (suite *basicSuite) TestLogStoreAPI() {
	tenantID := idgen.String()
	destinationID := idgen.Destination()
	eventID := idgen.Event()

	tests := []APITest{
		// Setup: Create tenant
		{
			Name: "PUT /:tenantID - Create tenant",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPUT,
				Path:   "/" + tenantID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		// Setup: Configure mock server destination
		{
			Name: "PUT mockserver/destinations",
			Request: httpclient.Request{
				Method:  httpclient.MethodPUT,
				BaseURL: suite.mockServerBaseURL,
				Path:    "/destinations",
				Body: map[string]interface{}{
					"id":   destinationID,
					"type": "webhook",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
					},
				},
			},
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
			},
		},
		// Setup: Create destination in Outpost
		{
			Name: "POST /:tenantID/destinations",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/" + tenantID + "/destinations",
				Body: map[string]interface{}{
					"id":     destinationID,
					"type":   "webhook",
					"topics": "*",
					"config": map[string]interface{}{
						"url": fmt.Sprintf("%s/webhook/%s", suite.mockServerBaseURL, destinationID),
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusCreated,
				},
			},
		},
		// Publish an event
		{
			Name: "POST /publish - Publish event",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodPOST,
				Path:   "/publish",
				Body: map[string]interface{}{
					"tenant_id":          tenantID,
					"topic":              "user.created",
					"eligible_for_retry": true,
					"id":                 eventID,
					"metadata": map[string]any{
						"source": "test",
					},
					"data": map[string]any{
						"user_id": "123",
						"email":   "test@example.com",
					},
				},
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusAccepted,
				},
			},
		},
		// Wait for event to be processed and logged, then list events
		{
			Name:  "GET /:tenantID/events - List events (with delay)",
			Delay: 3 * time.Second,
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/events",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"type":  "integer",
							"const": 200,
						},
						"body": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"data": map[string]interface{}{
									"type":     "array",
									"minItems": 1,
								},
								"count": map[string]interface{}{
									"type":    "integer",
									"minimum": 1,
								},
							},
							"required": []any{"data", "count"},
						},
					},
					"required": []any{"statusCode", "body"},
				},
			},
		},
		// Retrieve specific event
		{
			Name: "GET /:tenantID/events/:eventID - Retrieve event",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/events/" + eventID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"type":  "integer",
							"const": 200,
						},
						"body": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id": map[string]interface{}{
									"type":  "string",
									"const": eventID,
								},
								"tenant_id": map[string]interface{}{
									"type":  "string",
									"const": tenantID,
								},
								"topic": map[string]interface{}{
									"type":  "string",
									"const": "user.created",
								},
								"status": map[string]interface{}{
									"type": "string",
									"enum": []any{"pending", "success", "failed"},
								},
							},
							"required": []any{"id", "tenant_id", "topic"},
						},
					},
					"required": []any{"statusCode", "body"},
				},
			},
		},
		// List deliveries for the event
		{
			Name: "GET /:tenantID/events/:eventID/deliveries - List deliveries",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/events/" + eventID + "/deliveries",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"type":  "integer",
							"const": 200,
						},
						"body": map[string]interface{}{
							"type":     "array",
							"minItems": 1,
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"id": map[string]interface{}{
										"type": "string",
									},
									"status": map[string]interface{}{
										"type": "string",
										"enum": []any{"pending", "success", "failed"},
									},
									"delivered_at": map[string]interface{}{
										"type": "string",
									},
								},
								"required": []any{"id", "status", "delivered_at"},
							},
						},
					},
					"required": []any{"statusCode", "body"},
				},
			},
		},
		// List events by destination
		{
			Name: "GET /:tenantID/destinations/:destinationID/events - List events by destination",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationID + "/events",
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"type":  "integer",
							"const": 200,
						},
						"body": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"data": map[string]interface{}{
									"type":     "array",
									"minItems": 1,
								},
								"count": map[string]interface{}{
									"type":    "integer",
									"minimum": 1,
								},
							},
							"required": []any{"data", "count"},
						},
					},
					"required": []any{"statusCode", "body"},
				},
			},
		},
		// Retrieve event by destination
		{
			Name: "GET /:tenantID/destinations/:destinationID/events/:eventID - Retrieve event by destination",
			Request: suite.AuthRequest(httpclient.Request{
				Method: httpclient.MethodGET,
				Path:   "/" + tenantID + "/destinations/" + destinationID + "/events/" + eventID,
			}),
			Expected: APITestExpectation{
				Match: &httpclient.Response{
					StatusCode: http.StatusOK,
				},
				Validate: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"statusCode": map[string]interface{}{
							"type":  "integer",
							"const": 200,
						},
						"body": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id": map[string]interface{}{
									"type":  "string",
									"const": eventID,
								},
								"destination_id": map[string]interface{}{
									"type":  "string",
									"const": destinationID,
								},
							},
							"required": []any{"id", "destination_id"},
						},
					},
					"required": []any{"statusCode", "body"},
				},
			},
		},
	}

	suite.RunAPITests(suite.T(), tests)
}
